// Package transport carries framed binary messages between clients and the
// server. A transport never touches world state: it hands each accepted
// connection to a Handler, which turns messages into world commands.
package transport

import (
	"context"
	"net/http"
)

// Conn is one client connection, independent of the wire transport.
type Conn interface {
	// Recv blocks for the next inbound frame. It returns an error once the
	// connection is closed.
	Recv(ctx context.Context) ([]byte, error)
	// Send queues a frame without blocking. It reports false (and counts a
	// dropped frame) when the bounded send queue is full or the connection is
	// closed. The frame must not be modified afterwards.
	Send(frame []byte) bool
	// Close flushes queued frames, then closes the connection. Idempotent.
	Close()
	// Done is closed when the connection has ended.
	Done() <-chan struct{}
}

// Handler serves one connection. It runs in its own goroutine and the
// connection is closed when it returns.
type Handler func(Conn)

// Stats receives transport counters. ops.Metrics implements it.
type Stats interface {
	AddBytesOut(n int)
	AddDropped(n int)
}

// Transport is a way for clients to reach the server.
type Transport interface {
	// Name identifies the transport in logs, e.g. "websocket".
	Name() string
	// HTTPHandler returns the HTTP handler that accepts connections and
	// passes each one to h.
	HTTPHandler(h Handler) http.Handler
}
