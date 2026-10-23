package machines

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestRemoteApprovalRevisionIsMonotonicForRendererObservation(t *testing.T) {
	const id = "original-remote-task"
	s := &Service{cache: map[string][]api.WorkState{"machine": {{Task: api.Task{ID: id}}}}, observations: map[string]api.WorkObservation{}}
	first := api.Approval{ID: "worker-approval:original", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "answer"}}}
	s.observations[id] = api.WorkObservation{Revision: "z", Approvals: []api.Approval{first}}
	_, before := s.RetainedApprovalState()
	first.Status = "resolved"
	next := api.Approval{ID: "worker-approval:next", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "opaque", Details: "native opaque offer"}}}
	s.observations[id] = api.WorkObservation{Revision: "a", Approvals: []api.Approval{first, next}}
	views, after := s.RetainedApprovalState()
	if len(views) != 2 || after <= before || after >= 1<<53 {
		t.Fatalf("renderer would discard later original approval: before=%d after=%d views=%d", before, after, len(views))
	}
	// Native output revision changes do not invent an approval change.
	v := s.observations[id]
	v.Revision = "output-only"
	s.observations[id] = v
	if _, stable := s.RetainedApprovalState(); stable != after {
		t.Fatal("unchanged approval view changed its revision", after, stable)
	}
	delete(s.cache, "machine")
	if views, closed := s.RetainedApprovalState(); len(views) != 0 || closed <= after {
		t.Fatal("removing last approval did not advance projection", views, closed, after)
	}
}
