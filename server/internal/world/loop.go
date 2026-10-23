package world

import "context"

// Run ticks the world at the configured rate, driven by clock, until ctx is
// done. A tick in progress always completes before Run returns.
func (w *World) Run(ctx context.Context, clock Clock) {
	t := clock.NewTicker(w.TickInterval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C():
			w.Tick()
		}
	}
}
