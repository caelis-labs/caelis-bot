package caelis

import (
	"slices"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/sharedruntime"
)

// A transient selection of reads on the existing owner, never execution state.
// One exhausted journal/worker must not consume another object's opportunities.
type observationPlan map[string]bool

func (p observationPlan) includes(key string) bool { return p == nil || p[key] }

func (s *Session) sessionObservationKeyLocked(sid string) string {
	for id, w := range s.state.Workers {
		if w.Binding.SessionId == sid {
			return "worker:" + id
		}
	}
	return "main"
}

func (s *Session) workerNeedsObservationLocked(w worker) bool {
	if v := s.state.Views[w.Binding.SessionId]; v != nil && v.ApprovalDirty {
		return true
	}
	if w.Start != nil {
		return true
	}
	status := s.workerViewLocked(w).Status
	historical := w.Activity == "idle" && status != "pending" || status == "completed" || status == "interrupted" || status == "failed"
	return !historical && !s.streams[w.Binding.SessionId]
}

func (s *Session) pendingObservationsLocked() observationPlan {
	pending := observationPlan{}
	if !s.connected || s.client == nil {
		pending["connection"] = true
		return pending
	}
	if !s.retainedWorkers && s.configurationRecoveryPendingLocked() && !time.Now().Before(s.configurationRetryAt) {
		pending["configuration"] = true
	}
	for id, j := range s.state.Operations {
		if j.Outcome == "unknown" {
			pending["operation:"+id] = true
		}
	}
	for id, w := range s.state.Workers {
		if s.workerNeedsObservationLocked(w) {
			pending["worker:"+id] = true
		}
	}
	if !s.retainedWorkers {
		sid := s.state.Session.SessionId
		v := s.state.Views[sid]
		if v == nil || !s.streams[sid] || v.ApprovalDirty ||
			!value(v.State.Run.Active) && observedTurn(v) != "" && s.state.FinishedTurn != observedTurn(v) && slices.Contains([]string{"completed", "failed", "interrupted", "cancelled"}, value(v.State.Run.Status)) {
			pending["main"] = true
		}
	}
	return pending
}

func (s *Session) takeObservationsLocked(now time.Time) observationPlan {
	selected := observationPlan{}
	if s.observationBudgets == nil {
		s.observationBudgets = map[string]*sharedruntime.ObservationBudget{}
	}
	for key := range s.pendingObservationsLocked() {
		budget := s.observationBudgets[key]
		if budget == nil {
			budget = &sharedruntime.ObservationBudget{}
			s.observationBudgets[key] = budget
		}
		if budget.Take(now) {
			selected[key] = true
		}
	}
	return selected
}

// RetryObservations is the explicit host recovery action. Routine Connect calls
// only ensure the connection and cannot perpetually refill failed read budgets.
func (s *Session) RetryObservations() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.connected || s.client == nil {
		return
	}
	s.observationBudgets = nil
	s.requestRefreshLocked()
}
