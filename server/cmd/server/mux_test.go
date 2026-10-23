package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cleiderg/lush/server/internal/ops"
)

func TestNewMuxRoutes(t *testing.T) {
	ws := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	mux := newMux(ws, ops.NewMetrics("test").Handler(), nil)
	for path, want := range map[string]int{
		"/ws":          http.StatusTeapot,
		"/metrics":     200,
		"/healthz":     200,
		"/readyz":      200,
		"/api/version": 200,
		"/nope":        404,
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("%s = %d, want %d", path, rec.Code, want)
		}
	}
}
