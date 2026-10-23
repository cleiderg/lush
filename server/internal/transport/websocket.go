package transport

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	// DefaultSendQueue is the per-connection bounded send queue length.
	DefaultSendQueue = 64
	// MaxFrameBytes is the largest inbound frame accepted.
	MaxFrameBytes = 4096
	writeTimeout  = 5 * time.Second
)

// WebSocket is the Phase 1 transport.
type WebSocket struct {
	stats    Stats
	queueLen int
	origins  []string
}

// WebSocketOption customises a WebSocket transport.
type WebSocketOption func(*WebSocket)

// WithSendQueue sets the per-connection send queue length.
func WithSendQueue(n int) WebSocketOption {
	return func(w *WebSocket) {
		if n > 0 {
			w.queueLen = n
		}
	}
}

// WithOriginPatterns allows cross-origin upgrades from matching hosts.
func WithOriginPatterns(p ...string) WebSocketOption {
	return func(w *WebSocket) { w.origins = p }
}

// NewWebSocket creates the transport. stats may be nil.
func NewWebSocket(stats Stats, opts ...WebSocketOption) *WebSocket {
	w := &WebSocket{stats: stats, queueLen: DefaultSendQueue}
	for _, o := range opts {
		o(w)
	}
	return w
}

func (w *WebSocket) Name() string { return "websocket" }

// HTTPHandler upgrades requests and runs h on the connection until it returns.
func (w *WebSocket) HTTPHandler(h Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(rw, r, &websocket.AcceptOptions{OriginPatterns: w.origins})
		if err != nil {
			return // Accept already replied
		}
		ws.SetReadLimit(MaxFrameBytes)
		c := newWSConn(ws, w.stats, w.queueLen)
		defer c.Close()
		h(c)
	})
}

type wsConn struct {
	ws    *websocket.Conn
	stats Stats
	ctx   context.Context
	stop  context.CancelFunc

	mu     sync.Mutex
	closed bool
	queue  chan []byte
	done   chan struct{}
}

func newWSConn(ws *websocket.Conn, stats Stats, queueLen int) *wsConn {
	ctx, stop := context.WithCancel(context.Background())
	c := &wsConn{ws: ws, stats: stats, ctx: ctx, stop: stop,
		queue: make(chan []byte, queueLen), done: make(chan struct{})}
	go c.writeLoop()
	return c
}

func (c *wsConn) writeLoop() {
	defer close(c.done)
	failed := false
	for f := range c.queue {
		if failed {
			continue // drain so Send/Close never wait on us
		}
		ctx, cancel := context.WithTimeout(c.ctx, writeTimeout)
		err := c.ws.Write(ctx, websocket.MessageBinary, f)
		cancel()
		if err != nil {
			failed = true
			c.stop()
			_ = c.ws.CloseNow()
			continue
		}
		if c.stats != nil {
			c.stats.AddBytesOut(len(f))
		}
	}
	if !failed {
		_ = c.ws.Close(websocket.StatusNormalClosure, "")
	}
	c.stop()
}

func (c *wsConn) Recv(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-c.ctx.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	_, data, err := c.ws.Read(ctx)
	return data, err
}

func (c *wsConn) Send(frame []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	select {
	case c.queue <- frame:
		return true
	default:
		if c.stats != nil {
			c.stats.AddDropped(1)
		}
		return false
	}
}

func (c *wsConn) Close() {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.queue)
	}
	c.mu.Unlock()
	select {
	case <-c.done:
	case <-time.After(2 * writeTimeout):
		c.stop()
		_ = c.ws.CloseNow()
	}
}

func (c *wsConn) Done() <-chan struct{} { return c.done }
