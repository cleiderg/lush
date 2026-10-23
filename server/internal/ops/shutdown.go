package ops

import (
	"context"
	"fmt"
	"time"

	"github.com/cleiderg/lush/server/internal/world"
)

// ShutdownHooks are the actions run on SIGTERM.
type ShutdownHooks struct {
	// Announce broadcasts the restart countdown; called once per second with
	// the time remaining, starting with the full countdown.
	Announce func(remaining time.Duration)
	// Save persists every player. Called once after the countdown.
	Save func(ctx context.Context) error
	// Flush delivers buffered error reports. Called last, even if Save failed.
	Flush func(ctx context.Context)
}

// GracefulShutdown runs the countdown on clock, then the save hook. It
// returns early (still saving) if ctx is cancelled during the countdown, so a
// second signal or the stop timeout never skips the save.
func GracefulShutdown(ctx context.Context, clock world.Clock, countdown time.Duration, h ShutdownHooks) error {
	remaining := countdown
	if h.Announce != nil {
		h.Announce(remaining)
	}
	if remaining > 0 {
		t := clock.NewTicker(time.Second)
		defer t.Stop()
	loop:
		for remaining > 0 {
			select {
			case <-t.C():
				remaining -= time.Second
				if remaining < 0 {
					remaining = 0
				}
				if h.Announce != nil && remaining > 0 {
					h.Announce(remaining)
				}
			case <-ctx.Done():
				break loop
			}
		}
	}
	var err error
	if h.Save != nil {
		if e := h.Save(context.WithoutCancel(ctx)); e != nil {
			err = fmt.Errorf("save players: %w", e)
		}
	}
	if h.Flush != nil {
		h.Flush(context.WithoutCancel(ctx))
	}
	return err
}
