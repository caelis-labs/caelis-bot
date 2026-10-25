package tasks

import (
	"context"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
)

func (m *Manager) taskEnvironment(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.state.Records[id]; r != nil {
		return r.View.Machine
	}
	return ""
}

// The same Manager serializes each target's admissions, without holding its
// local ledger fence across peer I/O. No task/receipt authority lives in this map.
type environmentOperation struct {
	mu    sync.Mutex
	users int
}

func (m *Manager) environmentOperation(ctx context.Context, machine string) (func(), error) {
	if machine == "" {
		return func() {}, nil
	}
	m.environmentMu.Lock()
	if m.environmentOps == nil {
		m.environmentOps = map[string]*environmentOperation{}
	}
	gate := m.environmentOps[machine]
	if gate == nil {
		gate = &environmentOperation{}
		m.environmentOps[machine] = gate
	}
	gate.users++
	m.environmentMu.Unlock()
	drop := func() {
		m.environmentMu.Lock()
		defer m.environmentMu.Unlock()
		gate.users--
		if gate.users == 0 {
			delete(m.environmentOps, machine)
		}
	}
	if e := lockwait.Lock(ctx, &gate.mu); e != nil {
		drop()
		return nil, e
	}
	return func() { gate.mu.Unlock(); drop() }, nil
}

// Caller owns op and an environment admission lock. Ledger mutations stay under
// op; native I/O retains its original task/op/owner and can complete independently.
func workerIO[T any](m *Manager, machine string, call func() (T, error)) (T, error) {
	if machine == "" {
		return call()
	}
	m.op.Unlock()
	defer m.op.Lock()
	return call()
}
func workerAction(m *Manager, machine string, call func() error) error {
	_, e := workerIO(m, machine, func() (struct{}, error) { return struct{}{}, call() })
	return e
}
func (m *Manager) environmentUsageLocked(machine string) int {
	n := 0
	for _, r := range m.state.Records {
		if !m.owns(r) || r.View.Machine != machine {
			continue
		}
		if r.Activity == "active" {
			n++
			continue
		}
		switch r.View.Status {
		case "working", "running", "sending", "pending", "stopping", "interrupting", "awaiting_approval", "waiting_approval":
			n++
		case "unknown":
			if r.Activity != "idle" && r.Activity != api.WorkActivityOwnerUnavailable {
				n++
			}
		}
	}
	return n
}
