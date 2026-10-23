// Package chat is the local (proximity) chat system.
package chat

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cleiderg/lush/server/internal/world"
)

// Config holds chat tuning. Values come from the issue (#11).
type Config struct {
	Radius      float32       // delivery radius in world units, inclusive
	MaxBytes    int           // longest accepted message
	MinInterval time.Duration // minimum gap between a player's accepted messages
}

// DefaultConfig returns radius 25, 120 bytes, 1 message per second.
func DefaultConfig() Config {
	return Config{Radius: 25, MaxBytes: 120, MinInterval: time.Second}
}

// SendCommand is a ChatSend from a player.
type SendCommand struct {
	Player world.PlayerID
	Text   string
}

func (c SendCommand) Target() world.PlayerID { return c.Player }

// Message is a ChatMessage delivery.
type Message struct {
	From world.PlayerID
	Text string
}

// Sink receives deliveries; the network layer implements it. It is called on
// the tick goroutine and must not block.
type Sink interface {
	Deliver(to world.PlayerID, m Message)
}

type pending struct {
	from world.PlayerID
	text string
}

// System turns SendCommands into deliveries to nearby players.
type System struct {
	cfg     Config
	clock   world.Clock
	sink    Sink
	last    map[world.PlayerID]time.Time
	pending []pending
}

// New creates the chat system.
func New(cfg Config, clock world.Clock, sink Sink) *System {
	return &System{cfg: cfg, clock: clock, sink: sink, last: make(map[world.PlayerID]time.Time)}
}

func (s *System) Name() string { return "chat" }

// Sanitize strips control characters. It reports false if the message is too
// long, not valid UTF-8, or empty after stripping.
func Sanitize(text string, maxBytes int) (string, bool) {
	if len(text) > maxBytes || !utf8.ValidString(text) {
		return "", false
	}
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	return text, text != ""
}

// Handle validates and rate-limits; accepted messages are delivered on Tick.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	c, ok := cmd.(SendCommand)
	if !ok {
		return
	}
	text, ok := Sanitize(c.Text, s.cfg.MaxBytes)
	if !ok {
		return
	}
	now := s.clock.Now()
	if t, seen := s.last[p.ID]; seen && now.Sub(t) < s.cfg.MinInterval {
		return
	}
	s.last[p.ID] = now
	s.pending = append(s.pending, pending{p.ID, text})
}

// Tick delivers pending messages to every player within Radius of the
// speaker, the speaker included.
func (s *System) Tick(w *world.World, _ time.Duration) {
	for _, m := range s.pending {
		from := w.Player(m.from)
		if from == nil {
			continue
		}
		r2 := float64(s.cfg.Radius) * float64(s.cfg.Radius)
		for _, p := range w.Players() {
			dx := float64(p.Pos.X) - float64(from.Pos.X)
			dy := float64(p.Pos.Y) - float64(from.Pos.Y)
			if dx*dx+dy*dy <= r2 {
				s.sink.Deliver(p.ID, Message{From: m.from, Text: m.text})
			}
		}
	}
	s.pending = s.pending[:0]
	for id := range s.last {
		if w.Player(id) == nil {
			delete(s.last, id)
		}
	}
}
