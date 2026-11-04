package session

import (
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/gather"
	"github.com/cleiderg/lush/server/internal/world"
)

// SetTokenSource replaces how s generates new guest tokens.
func SetTokenSource(s *Server, f func() (string, error)) { s.newToken = f }

// Disconnected reports whether id is in the world with no connection, waiting
// out the logout lock. Call it from the goroutine that ticks the world.
func Disconnected(s *Server, id world.PlayerID) bool { return s.gone[id] != nil }

// DecodedMessage is a decoded client message.
type DecodedMessage struct{ m message }

// Command is the command m asks for on behalf of id.
func (d DecodedMessage) Command(id world.PlayerID) world.Command { return d.m.command(id) }

// DecodeForTest decodes a client envelope.
func DecodeForTest(buf []byte) (DecodedMessage, error) {
	m, err := decode(buf)
	return DecodedMessage{m}, err
}

// CheckSkillCodes fails t if a gather start result or stop reason has no
// SkillResult code.
func CheckSkillCodes(t *testing.T) {
	for r := gather.Started; r <= gather.Felled; r++ {
		if _, ok := startCodes[r]; !ok {
			t.Errorf("start result %v has no code", r)
		}
	}
	for w := gather.StoppedByPlayer; w <= gather.StoppedLevel; w++ {
		if _, ok := stopCodes[w]; !ok {
			t.Errorf("stop reason %v has no code", w)
		}
	}
}
