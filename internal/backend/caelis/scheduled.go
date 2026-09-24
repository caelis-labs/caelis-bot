package caelis

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/activation"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Background turns are correlated through native command targets. Safe-point
// input IDs additionally classify inputs; prose never establishes provenance.
func (s *Session) applyScheduledEnvelope(v *view, e wire.Envelope) {
	turn := value(e.TurnId)
	id := value(e.InputOperationId)
	j := s.state.Operations[id]
	scheduled := turn != "" && j.Scheduled
	if scheduled {
		j.TurnID = turn
		s.state.Operations[id] = j
	}
	applyEnvelope(v, e, scheduled)
	if turn != "" && e.Lifecycle != nil && e.Kind == "caelis/lifecycle" && value(e.ApprovalRequestId) == "" && (value(e.Scope) == "" || value(e.Scope) == "main") {
		if v.Turns == nil {
			v.Turns = map[string]string{}
		}
		v.Turns[turn] = e.Lifecycle.State
	}
}
func (s *Session) presentScheduled(out api.Snapshot, v *view) api.Snapshot {
	turns := map[string]string{}
	for _, j := range s.state.Operations {
		if j.Scheduled && j.TurnID != "" {
			status := v.Turns[j.TurnID]
			if j.TurnID == out.CurrentTurn {
				status = value(v.State.Run.Status)
			}
			turns[j.TurnID] = status
		}
	}
	pending := s.sendingScheduled != ""
	if pending && out.CurrentTurn != s.scheduledPreviousTurn {
		turns[out.CurrentTurn] = "running"
	}
	return activation.Present(out, turns, pending)
}
