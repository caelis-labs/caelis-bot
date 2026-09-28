// Package contextseed retains the first input until native acceptance. Both
// adapters store it inside their existing durable dispatch journal.
package contextseed

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type State struct {
	Injected bool   `json:"injected,omitempty"`
	Pending  *Input `json:"pending,omitempty"`
}
type Input struct {
	ID       string          `json:"id"`
	Seed     api.ContextSeed `json:"seed"`
	Accepted bool            `json:"accepted,omitempty"`
}

func (s *State) Prepare(ctx context.Context, c *api.ToolConnection, id string) (string, error) {
	if s.Injected || c == nil || c.PrepareContext == nil {
		return "", nil
	}
	if s.Pending != nil {
		if s.Pending.ID != id {
			return "", errors.New("上次上下文投递尚未确认")
		}
		return s.Pending.Seed.Text, nil
	}
	seed, err := c.PrepareContext(ctx)
	if err != nil {
		return "", err
	}
	s.Pending = &Input{ID: id, Seed: seed}
	return seed.Text, nil
}

func (s *State) Resolve(id, outcome string) {
	if s.Pending == nil || s.Pending.ID != id {
		return
	}
	switch outcome {
	case "accepted":
		s.Injected, s.Pending.Accepted = true, true
	case "rejected":
		s.Pending = nil
	}
}

// Cleanup must run only after Resolve has been saved. Failure retains a receipt
// for reconnect/retry; a changed handoff must never be removed.
func (s *State) Cleanup(c *api.ToolConnection) error {
	if s.Pending == nil || !s.Pending.Accepted {
		return nil
	}
	if c != nil && c.ConsumeContext != nil {
		if err := c.ConsumeContext(s.Pending.Seed); err != nil {
			return err
		}
	}
	s.Pending = nil
	return nil
}
