package session

import "github.com/cleiderg/lush/server/internal/world"

// SetTokenSource replaces how s generates new guest tokens.
func SetTokenSource(s *Server, f func() (string, error)) { s.newToken = f }

// Disconnected reports whether id is in the world with no connection, waiting
// out the logout lock. Call it from the goroutine that ticks the world.
func Disconnected(s *Server, id world.PlayerID) bool { return s.gone[id] != nil }
