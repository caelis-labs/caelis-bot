package codex

import (
	"regexp"
	"time"

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

func (s *Session) captureBackgroundResults() bool {
	changed := false
	for id, turn := range s.binding.Scheduled {
		if turn == "" {
			continue
		}
		if _, dream := s.binding.Dreams[id]; dream {
			continue
		}
		old := s.binding.BackgroundResults[id]
		if old.Complete {
			continue
		}
		approval := false
		for _, p := range s.prompts {
			if p.thread == s.binding.ThreadID && p.turn == turn && p.view.Status != "resolved" {
				approval = true
			}
		}
		next := activation.Observe(old, id, opaque(turn), s.runs[turn], s.state.Items, approval, time.Now())
		if next == old || !next.Visible && !next.Complete {
			continue
		}
		if s.binding.BackgroundResults == nil {
			s.binding.BackgroundResults = map[string]api.BackgroundResult{}
		}
		s.binding.BackgroundResults[id] = next
		changed = true
	}
	return changed
}
func (s *Session) BackgroundResult(id string) api.BackgroundResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.binding.BackgroundResults[id]
	out.ID = id
	return out
}
