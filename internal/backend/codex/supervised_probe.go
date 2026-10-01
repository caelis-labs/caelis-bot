package codex

import (
	"context"
	"errors"
	"time"
)

// ProbeSupervisedAuth validates the pinned helper, native power binding and
// owned public stdio/auth handshake before original-source retirement. It
// creates no thread, activation, tool grant, model turn or durable APP binding.
func ProbeSupervisedAuth(ctx context.Context, helper, binary, directory string) (status AuthStatus, err error) {
	s := NewSession(SessionOptions{ForceOwned: true, WatchdogHelper: helper})
	if err = s.ConfigureOwnedDeadline(ctx, "read-only-native-preflight", time.Now().Add(45*time.Second)); err != nil {
		return status, err
	}
	c, err := s.startSupervised(ctx, Options{Binary: binary, Directory: directory})
	if err != nil {
		return status, err
	}
	s.mu.Lock()
	s.client = c
	s.mu.Unlock()
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		err = errors.Join(err, s.FenceStop(stop))
	}()
	return c.ReadAuthStatus(ctx)
}
