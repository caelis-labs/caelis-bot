package codex

import "context"

// SharedConnectionDetails reads the active standard App Server. It never starts
// another process or logs credentials. CLI setup can report a different account
// when an existing shared server was launched from another installation.
func (s *Session) SharedConnectionDetails(ctx context.Context) (AuthStatus, string, bool, error) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil || !client.shared {
		return AuthStatus{}, "", false, nil
	}
	auth, err := client.ReadAuthStatus(ctx)
	if err != nil {
		return AuthStatus{}, client.userAgent, true, err
	}
	return auth, client.userAgent, true, nil
}
