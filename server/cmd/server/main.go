// Command server is the Lush game server.
package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
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
	"github.com/cleiderg/lush/server/internal/systems/emote"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/grounditems"
	"github.com/cleiderg/lush/server/internal/systems/items"
	"github.com/cleiderg/lush/server/internal/systems/pvp"
	"github.com/cleiderg/lush/server/internal/systems/regen"
	"github.com/cleiderg/lush/server/internal/systems/shop"
	"github.com/cleiderg/lush/server/internal/systems/trade"
	"github.com/cleiderg/lush/server/internal/transport"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
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
	reporter, err := ops.NewReporter(cfg, version, nil)
	if err != nil {
		log.Error("bad config", "err", err)
		return 2
	}
	// Deliver error reports logged on any exit path before main calls os.Exit.
	defer reporter.Flush(context.Background())
	log = slog.New(reporter.WrapHandler(log.Handler()))
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

	mover, grid, err := newMover(cfg.MoveSpeed, cfg.RunSpeed)
	if err != nil {
		log.Error("movement setup failed", "err", err)
		return 1
	}
	data, err := gamedata.Default()
	if err != nil {
		log.Error("game data", "err", err)
		return 1
	}
	sess := session.New(sessionConfig(cfg, grid, data, reporter.Recovered), w, clock, players, log)
	lookup, err := areas.Load()
	if err != nil {
		log.Error("areas", "err", err)
		return 1
	}
	systems, err := gameSystems(mover, data, lookup, grid.CellSize(), clock, sess)
	if err != nil {
		log.Error("system setup failed", "err", err)
		return 1
	}
	for _, s := range systems {
		if err := w.RegisterEvery(s.sys, s.every); err != nil {
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
		Flush: reporter.Flush,
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

// combatEvery is the combat tick period in world ticks (0.6 s at 20 Hz).
const combatEvery = 12

// scheduled is a world system and the period, in world ticks, of its Tick.
type scheduled struct {
	sys   world.System
	every int
}

// gameSystems lists the world systems in registration order. Order matters:
// movement applies inputs, emote overrides animation, chat delivers, items
// and shop apply item actions and trades with the stall (whose results the
// session sends at once), trade runs player trades and commits them through
// the session's Committer, ground items expire, PvP combat swings on the
// combat tick (its hits and drops go to the session, which spawns the drops
// as ground items), regen heals (SW1), and the session sends snapshots,
// combat hits, stats and inventory last. tileSize is the collision grid's
// cell size in metres, the unit of the shop, trade, pick-up and attack
// distances.
func gameSystems(mover world.System, data *gamedata.Data, lookup *areas.Lookup, tileSize float64, clock world.Clock, sess *session.Server) ([]scheduled, error) {
	heal, err := regen.New(data.Rules, clock)
	if err != nil {
		return nil, err
	}
	trades := trade.New(data, lookup, tileSize, sess, sess)
	sess.SetTrades(trades)
	ground := grounditems.New(data, clock, tileSize)
	sess.SetGroundItems(ground)
	fights, err := pvp.NewCombat(data, lookup, tileSize, clock, rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())), sess, sess)
	if err != nil {
		return nil, err
	}
	sess.SetPvP(fights)
	out := []scheduled{}
	for _, s := range []world.System{mover, emote.New(emote.DefaultConfig()), chat.New(chat.DefaultConfig(), clock, sess),
		items.New(data), shop.New(data, lookup, tileSize, sess), trades, ground} {
		out = append(out, scheduled{s, 1})
	}
	return append(out, scheduled{fights, combatEvery}, scheduled{heal, 1}, scheduled{sess, 1}), nil
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

// sessionConfig is the session tuning from the server config. The walk and
// run speeds are the same ones newMover gets, so Welcome tells clients the
// speeds the server enforces (D-044).
func sessionConfig(cfg ops.Config, grid *world.Grid, data *gamedata.Data, onPanic func(any)) session.Config {
	return session.Config{
		TickRate:     cfg.TickRate,
		Spawn:        world.Vec2{X: cfg.SpawnX, Y: cfg.SpawnZ},
		Collision:    grid,
		MoveSpeed:    cfg.MoveSpeed,
		RunSpeed:     cfg.RunSpeed,
		SaveInterval: cfg.SaveInterval,
		Data:         data,
		OnPanic:      onPanic,
	}
}
