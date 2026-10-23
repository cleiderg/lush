// Package api serves the server's plain HTTP endpoints (readiness, version).
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cleiderg/lush/server/internal/protocol"
)

// Pinger reports whether a dependency (the database) is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Config configures the API handlers.
type Config struct {
	// Version is the build version (main.version).
	Version string
	// Pinger is optional; with none, /readyz reports no database configured.
	Pinger Pinger
}

const pingTimeout = 2 * time.Second

// Mount registers the API routes on mux.
func Mount(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc("/readyz", getOnly(cfg.readyz))
	mux.HandleFunc("/api/version", getOnly(cfg.version))
}

func getOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h(w, r)
	}
}

func (c Config) readyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if c.Pinger == nil {
		_, _ = w.Write([]byte("ok: no database configured\n"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
	defer cancel()
	if err := c.Pinger.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready: database unreachable\n"))
		return
	}
	_, _ = w.Write([]byte("ok\n"))
}

type versionResponse struct {
	Protocol int    `json:"protocol"`
	Build    string `json:"build"`
}

func (c Config) version(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(versionResponse{
		Protocol: int(protocol.ProtocolVersionCurrent),
		Build:    c.Version,
	})
}
