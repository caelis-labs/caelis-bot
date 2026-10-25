package machines

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
	"sync"
)

// Ephemeral serialization of one SSH profile's pin/credential/config changes.
// It contains no task, receipt or execution state and never gates local control.
type connectionGate struct {
	mu    sync.Mutex
	users int
}

func (s *Service) lockSSHConnection(ctx context.Context, id string) (func(), error) {
	s.connectionMu.Lock()
	if s.connections == nil {
		s.connections = map[string]*connectionGate{}
	}
	gate := s.connections[id]
	if gate == nil {
		gate = &connectionGate{}
		s.connections[id] = gate
	}
	gate.users++
	s.connectionMu.Unlock()
	drop := func() {
		s.connectionMu.Lock()
		defer s.connectionMu.Unlock()
		gate.users--
		if gate.users == 0 {
			delete(s.connections, id)
		}
	}
	if e := lockwait.Lock(ctx, &gate.mu); e != nil {
		drop()
		return nil, e
	}
	return func() { gate.mu.Unlock(); drop() }, nil
}
