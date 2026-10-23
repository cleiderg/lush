package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWebSocketOriginPatterns(t *testing.T) {
	ws := NewWebSocket(nil, WithOriginPatterns("play.example.com"))
	srv := httptest.NewServer(ws.HTTPHandler(func(c Conn) { c.Send([]byte("ok")) }))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	for _, tc := range []struct {
		origin string
		ok     bool
	}{{"", true}, {"https://play.example.com", true}, {"https://evil.example.com", false}} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		h := http.Header{}
		if tc.origin != "" {
			h.Set("Origin", tc.origin)
		}
		c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h})
		if (err == nil) != tc.ok {
			t.Errorf("origin %q: dial err = %v, want ok %v", tc.origin, err, tc.ok)
		}
		if err == nil {
			if _, msg, err := c.Read(ctx); err != nil || string(msg) != "ok" {
				t.Errorf("origin %q: read %q, %v", tc.origin, msg, err)
			}
			c.CloseNow()
		}
		cancel()
	}
}
