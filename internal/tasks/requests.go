package tasks

import (
	"context"
	"errors"
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// ReadTaskRequest resolves only an existing ledger record. A missing/ambiguous
// record is uncertainty, never authority to create another worker.
func (m *Manager) ReadTaskRequest(ctx context.Context, request string) (api.Task, error) {
	if len(request) < 8 || len(request) > 128 {
		return api.Task{}, errors.New("invalid original requestId")
	}
	m.op.Lock()
	m.mu.Lock()
	id := ""
	for key, r := range m.state.Records {
		if !m.owns(r) {
			continue
		}
		found := slices.Contains(r.Requests, request)
		// Historical ledgers encode start identity in the opaque task ID.
		for _, owner := range []string{"local", r.Provider, m.provider, "codex", "caelis"} {
			found = found || key == "task-"+hash(owner, request)
		}
		found = found || key == "task-"+hash(request)
		if r.View.Machine != "" {
			found = found || key == "task-"+hash("remote", r.View.Machine, request)
		}
		if found {
			if id != "" && id != key {
				m.mu.Unlock()
				m.op.Unlock()
				return api.Task{}, errors.New("requestId matches multiple owned tasks; use exact task handle")
			}
			id = key
		}
	}
	m.mu.Unlock()
	m.op.Unlock()
	if id == "" {
		return api.Task{}, errors.New("original request not found; outcome unconfirmed, do not replay")
	}
	return m.ReadTask(ctx, id)
}
