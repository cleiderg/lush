// Command server is the Lush game server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cleiderg/lush/server/internal/ops"
	"github.com/cleiderg/lush/server/internal/world"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() { os.Exit(run()) }

func run() int {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := ops.LoadConfig(os.Getenv)
	if err != nil {
		log.Error("bad config", "err", err)
		return 2
	}
	log.Info("starting", "version", version, "env", cfg.Env, "listen", cfg.ListenAddr, "tick_rate", cfg.TickRate)

	metrics := ops.NewMetrics(version)
	w := world.New(world.Config{TickRate: cfg.TickRate})
	clock := realClock{}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: metrics.Handler(), ReadHeaderTimeout: 5 * time.Second}
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
		Save:     func(context.Context) error { log.Info("saving players"); return nil },
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

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) NewTicker(d time.Duration) world.Ticker {
	return realTicker{time.NewTicker(d)}
}

type realTicker struct{ *time.Ticker }

func (t realTicker) C() <-chan time.Time { return t.Ticker.C }
