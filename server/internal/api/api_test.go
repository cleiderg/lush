package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cleiderg/lush/server/internal/protocol"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func TestReadyz(t *testing.T) {
	tests := []struct {
		name   string
		pinger Pinger
		method string
		code   int
		body   string
	}{
		{"no pinger", nil, "GET", 200, "no database configured"},
		{"pinger ok", fakePinger{}, "GET", 200, "ok"},
		{"pinger fails", fakePinger{errors.New("down")}, "GET", 503, "not ready"},
		{"wrong method", nil, "POST", 405, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			Mount(mux, Config{Pinger: tt.pinger})
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tt.method, "/readyz", nil))
			if rec.Code != tt.code {
				t.Fatalf("code = %d, want %d", rec.Code, tt.code)
			}
			if !strings.Contains(rec.Body.String(), tt.body) {
				t.Fatalf("body = %q, want to contain %q", rec.Body.String(), tt.body)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	tests := []struct {
		name   string
		method string
		code   int
	}{
		{"get", "GET", 200},
		{"post", "POST", 405},
		{"delete", "DELETE", 405},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			Mount(mux, Config{Version: "1.2.3"})
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tt.method, "/api/version", nil))
			if rec.Code != tt.code {
				t.Fatalf("code = %d, want %d", rec.Code, tt.code)
			}
			if tt.code != 200 {
				return
			}
			var got struct {
				Protocol int    `json:"protocol"`
				Build    string `json:"build"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Protocol != int(protocol.ProtocolVersionCurrent) || got.Build != "1.2.3" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}
