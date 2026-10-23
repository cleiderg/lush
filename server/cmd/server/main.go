// Command server is the Lush game server.
package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cleiderg/lush/server/internal/api"
	"github.com/cleiderg/lush/server/internal/ops"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/chat"
	"github.com/cleiderg/lush/server/internal/systems/movement"
	"github.com/cleiderg/lush/server/internal/transport"
	"github.com/cleiderg/lush/server/internal/world"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

// saveTimeout bounds the final save so a stuck database cannot outlast the systemd stop timeout.
const saveTimeout = 20 * time.Second

// dbStartTimeout bounds connecting and migrating at startup.
const dbStartTimeout = 30 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	code := run(ctx, os.Getenv, realClock{}, os.Stdout)
	stop()
	os.Exit(code)
}

// run starts the server and blocks until ctx is cancelled (SIGTERM/SIGINT in
// production), then runs the graceful shutdown. It returns the exit code.
func run(ctx context.Context, getenv func(string) string, clock world.Clock, out io.Writer) int {
	log := slog.New(slog.NewJSONHandler(out, nil))
	cfg, err := ops.LoadConfig(getenv)
	if err != nil {
		log.Error("bad config", "err", err)
		return 2
	}
	log.Info("starting", "version", version, "env", cfg.Env, "listen", cfg.ListenAddr, "tick_rate", cfg.TickRate)

	var players store.Players
	var pinger api.Pinger
	if cfg.DatabaseURL != "" {
		// Errors from pgx can embed the connection string; log only the stage.
		pool, err := openDatabase(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Error("database unavailable", "stage", err.Error())
			return 1
		}
		defer pool.Close() // runs after the shutdown save
		players, pinger = store.NewPGPlayers(pool), pool
	} else if cfg.Env == "production" {
		log.Error("bad config", "err", "LUSH_DATABASE_URL is required when LUSH_ENV=production")
		return 2
	} else {
		log.Warn("LUSH_DATABASE_URL not set; using in-memory store, progress is lost on restart")
		players = store.NewMemory()
	}

	metrics := ops.NewMetrics(version)
	w := world.New(world.Config{TickRate: cfg.TickRate})

	mover, err := movement.New(movement.Config{MaxSpeed: cfg.MoveSpeed})
	if err != nil {
		log.Error("bad config", "err", err)
		return 2
	}
	sess := session.New(session.Config{
		TickRate:     cfg.TickRate,
		Spawn:        world.Vec2{X: cfg.SpawnX, Y: cfg.SpawnZ},
		MoveSpeed:    cfg.MoveSpeed,
		SaveInterval: cfg.SaveInterval,
	}, w, clock, players, log)
	// Order matters: movement applies inputs, chat delivers, session snapshots last.
	for _, s := range []world.System{mover, chat.New(chat.DefaultConfig(), clock, sess), sess} {
		if err := w.Register(s); err != nil {
			log.Error("register system", "err", err)
			return 2
		}
	}
	ws := transport.NewWebSocket(metrics, transport.WithOriginPatterns(cfg.WSOrigins...))
	mux := newMux(ws.HTTPHandler(sess.ServeConn), metrics.Handler(), pinger)

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.ListenAndServe() }()

	loopCtx, cancelLoop := context.WithCancel(context.Background())
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		t := clock.NewTicker(w.TickInterval())
		defer t.Stop()
		sample := clock.NewTicker(time.Second)
		defer sample.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-t.C():
				start := time.Now()
				w.Tick()
				metrics.RecordTick(time.Since(start))
				metrics.SetPlayers(len(w.Players()))
			case now := <-sample.C():
				metrics.Sample(now)
			}
		}
	}()

	select {
	case err := <-srvErr:
		log.Error("http server failed", "err", err)
		cancelLoop()
		<-loopDone
		return 1
	case <-ctx.Done():
	}

	log.Info("shutdown requested", "countdown", cfg.ShutdownCountdown.String())
	err = ops.GracefulShutdown(context.Background(), clock, cfg.ShutdownCountdown, ops.ShutdownHooks{
		Announce: func(r time.Duration) {
			log.Info("restart countdown", "seconds", int(r.Seconds()))
			sess.BroadcastRestartCountdown(r)
		},
		Save: func(ctx context.Context) error {
			sess.BroadcastReload("server restarting")
			log.Info("saving players")
			ctx, cancel := context.WithTimeout(ctx, saveTimeout)
			defer cancel()
			cancelLoop() // stop the tick so the snapshot can read world state
			<-loopDone
			return sess.SaveAll(ctx)
		},
	})
	cancelLoop()
	<-loopDone
	sess.Close()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := srv.Shutdown(shutCtx); e != nil && !errors.Is(e, http.ErrServerClosed) {
		log.Error("http shutdown", "err", e)
	}
	if err != nil {
		log.Error("shutdown", "err", err)
		return 1
	}
	log.Info("bye")
	return 0
}

// openDatabase connects a pool and applies pending migrations. The returned
// error names the failed stage only, never the URL.
func openDatabase(ctx context.Context, url string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbStartTimeout)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, errors.New("invalid database URL")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("connect failed")
	}
	if err := store.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, errors.New("migrate failed")
	}
	return pool, nil
}

// newMux mounts the WebSocket, API and metrics routes on one mux. pinger
// (nil without a database) backs /readyz.
func newMux(wsHandler, opsHandler http.Handler, pinger api.Pinger) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/ws", wsHandler)
	api.Mount(mux, api.Config{Version: version, Pinger: pinger})
	mux.Handle("/", opsHandler)
	return mux
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) NewTicker(d time.Duration) world.Ticker {
	return realTicker{time.NewTicker(d)}
}

type realTicker struct{ *time.Ticker }

func (t realTicker) C() <-chan time.Time { return t.Ticker.C }
