package codex

import (
	"sort"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// A report can start a new Turn without a visible user message. Keep its native
// receipt binding even when recovery returns only an assistant summary.
type hostReportTurn struct {
	Thread string `json:"thread"`
	Turn   string `json:"turn"`
	Phase  string `json:"phase"`
}

func (s *Session) rememberHostReportTurn(id, turn, phase string) {
	if !s.binding.HostReports[id] || s.binding.ThreadID == "" || turn == "" {
		return
	}
	old, exists := s.binding.HostReportTurns[id]
	if exists && (old.Thread != s.binding.ThreadID || old.Turn != turn) {
		return // a different native target cannot replace an original receipt
	}
	if terminal(old.Phase) && !terminal(phase) {
		phase = old.Phase
	}
	next := hostReportTurn{Thread: s.binding.ThreadID, Turn: turn, Phase: phase}
	if old == next {
		return
	}
	if s.binding.HostReportTurns == nil {
		s.binding.HostReportTurns = map[string]hostReportTurn{}
	}
	s.binding.HostReportTurns[id] = next
	if s.save() != nil {
		if exists {
			s.binding.HostReportTurns[id] = old
		} else {
			delete(s.binding.HostReportTurns, id)
		}
	}
}

func (s *Session) reportSources() []api.ReportSource {
	var out []api.ReportSource
	for id, source := range s.binding.HostReportTurns {
		if source.Thread != "" && source.Thread == s.binding.ThreadID && s.binding.HostReports[id] {
			out = append(out, api.ReportSource{RuntimeOwner: opaque("codex", source.Thread), TurnKey: opaque(source.Turn), ReportID: id, Phase: source.Phase})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReportID < out[j].ReportID })
	return out
}
