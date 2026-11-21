package server

// SetupToken returns the one-time first-run setup token, or "" once an Owner
// exists or setup has completed. It exists so tests and deployment tooling can
// present the token without weakening the HTTP path.
func (s *Server) SetupToken() string {
	if s.authService == nil {
		return ""
	}
	return s.authService.CurrentSetupToken()
}
