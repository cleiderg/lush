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

	metrics := ops.NewMetrics(version)
	w := world.New(world.Config{TickRate: cfg.TickRate})

	mover, err := movement.New(movement.Config{MaxSpeed: cfg.MoveSpeed})
	if err != nil {
		log.Error("bad config", "err", err)
		return 2
	}
	sess := session.New(session.Config{
		TickRate:  cfg.TickRate,
		Spawn:     world.Vec2{X: cfg.SpawnX, Y: cfg.SpawnZ},
		MoveSpeed: cfg.MoveSpeed,
	}, w, clock, store.NewMemory(), log) // pgx store comes in P7
	// Order matters: movement applies inputs, chat delivers, session snapshots last.
	for _, s := range []world.System{mover, chat.New(chat.DefaultConfig(), clock, sess), sess} {
		if err := w.Register(s); err != nil {
			log.Error("register system", "err", err)
			return 2
		}
	}
	ws := transport.NewWebSocket(metrics, transport.WithOriginPatterns(cfg.WSOrigins...))
	mux := newMux(ws.HTTPHandler(sess.ServeConn), metrics.Handler())

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
	// Broadcast and save are placeholders until transport and store land.
	err = ops.GracefulShutdown(context.Background(), clock, cfg.ShutdownCountdown, ops.ShutdownHooks{
		Announce: func(r time.Duration) { log.Info("restart countdown", "seconds", int(r.Seconds())) },
		Save: func(context.Context) error {
			log.Info("saving players") // placeholder until the store lands
			sess.BroadcastReload("server restarting")
			return nil
		},
	})
	cancelLoop()
	<-loopDone
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

// newMux mounts the WebSocket, API and metrics routes on one mux. The API has
// no Pinger yet; the database arrives with the store.
func newMux(wsHandler, opsHandler http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/ws", wsHandler)
	api.Mount(mux, api.Config{Version: version})
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
