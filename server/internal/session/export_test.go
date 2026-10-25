package session

// SetTokenSource replaces how s generates new guest tokens.
func SetTokenSource(s *Server, f func() (string, error)) { s.newToken = f }
