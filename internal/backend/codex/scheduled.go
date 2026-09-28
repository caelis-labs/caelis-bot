package codex

import (
	"regexp"

	"github.com/caelis-labs/caelis-bot/internal/backend/activation"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Session) presentScheduled(v api.Snapshot) api.Snapshot {
	turns := map[string]string{}
	for _, turn := range s.binding.Scheduled {
		if turn != "" {
			turns[opaque(turn)] = s.runs[turn]
		}
	}
	pending := false
	if p := s.binding.Pending; p != nil {
		_, pending = s.binding.Scheduled[p.ID]
	}
	if pending && v.CurrentTurn != "" {
		turns[v.CurrentTurn] = "inProgress"
	}
	v = activation.Present(v, turns, pending)
	dreams := map[string]string{}
	for id := range s.binding.Dreams {
		if turn := s.binding.Scheduled[id]; turn != "" {
			dreams[opaque(turn)] = s.runs[turn]
		}
	}
	dreamPending := false
	if p := s.binding.Pending; p != nil {
		_, dreamPending = s.binding.Dreams[p.ID]
	}
	return activation.Dream(v, dreams, dreamPending)
}

// Pre-provenance Bot builds reserved wake-<crypto/rand.Text> client IDs. Read
// those retained native IDs to repair old presentation without rewriting history.
// This compatibility remains until pre-provenance Bot histories are unsupported.
var legacyWakeID = regexp.MustCompile(`^wake-[A-Z2-7]{26}$`)

func (s *Session) BackgroundReceipt(id string) api.Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding.LastReceipt != nil && s.binding.LastReceipt.ID == id {
		return *s.binding.LastReceipt
	}
	if s.binding.Scheduled[id] != "" {
		return api.Receipt{ID: id, Outcome: "accepted"}
	}
	return api.Receipt{ID: id, Outcome: "unknown"}
}
