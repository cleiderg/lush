package ops

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
)

// Reporter sends panics and error-level logs to Sentry. A nil *Reporter is
// valid and does nothing, which is what a server without LUSH_SENTRY_DSN gets.
type Reporter struct {
	hub *sentry.Hub
}

// NewReporter returns nil (no client, no transport) when cfg.SentryDSN is
// empty. tr overrides the HTTP transport; tests pass a fake, production nil.
func NewReporter(cfg Config, release string, tr sentry.Transport) (*Reporter, error) {
	if cfg.SentryDSN == "" {
		return nil, nil
	}
	c, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:         cfg.SentryDSN,
		Environment: cfg.Env,
		Release:     release,
		Transport:   tr,
	})
	if err != nil {
		// sentry-go errors may quote the DSN; do not pass them on.
		return nil, fmt.Errorf("init sentry: invalid LUSH_SENTRY_DSN")
	}
	return &Reporter{hub: sentry.NewHub(c, sentry.NewScope())}, nil
}

// Recovered reports a value returned by recover().
func (r *Reporter) Recovered(v any) {
	if r == nil {
		return
	}
	r.hub.Recover(v)
}

// Flush waits for buffered events to be delivered, up to ctx's deadline
// (or 2s if it has none).
func (r *Reporter) Flush(ctx context.Context) {
	if r == nil {
		return
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}
	r.hub.Client().FlushWithContext(ctx)
}

// WrapHandler returns h with error-level records also sent to Sentry.
func (r *Reporter) WrapHandler(h slog.Handler) slog.Handler {
	if r == nil {
		return h
	}
	return &sentryHandler{Handler: h, r: r}
}

type sentryHandler struct {
	slog.Handler
	r     *Reporter
	attrs []slog.Attr
}

func (h *sentryHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.Handler.Enabled(ctx, l)
}

func (h *sentryHandler) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Level >= slog.LevelError {
		ev := sentry.NewEvent()
		ev.Level = sentry.LevelError
		ev.Message = rec.Message
		ev.Tags = map[string]string{}
		for _, a := range h.attrs {
			ev.Tags[a.Key] = a.Value.Resolve().String()
		}
		rec.Attrs(func(a slog.Attr) bool {
			ev.Tags[a.Key] = a.Value.Resolve().String()
			return true
		})
		h.r.hub.CaptureEvent(ev)
	}
	return h.Handler.Handle(ctx, rec)
}

func (h *sentryHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return &sentryHandler{Handler: h.Handler.WithAttrs(as), r: h.r, attrs: append(append([]slog.Attr(nil), h.attrs...), as...)}
}

func (h *sentryHandler) WithGroup(name string) slog.Handler {
	return &sentryHandler{Handler: h.Handler.WithGroup(name), r: h.r, attrs: h.attrs}
}
