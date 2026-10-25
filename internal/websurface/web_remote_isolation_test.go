package websurface

import (
	"context"
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type isolationReviewDecisionEngine struct {
	testEngine
	localDecisions int
}

func (e *isolationReviewDecisionEngine) Decide(context.Context, api.Decision) error {
	e.localDecisions++
	return nil
}

type isolationReviewRemoteDecision struct{ calls int }

func (e *isolationReviewRemoteDecision) DecideRetainedApproval(_ context.Context, d api.Decision) (bool, error) {
	if d.ID != "remote-original-request" {
		return false, nil
	}
	e.calls++
	return true, errors.New("native acknowledgement unavailable")
}

func TestReviewUnknownRemoteDecisionCannotGateLocalUpdate(t *testing.T) {
	e := &isolationReviewDecisionEngine{}
	s := testServer(t, e)
	c := pairClient(t, s)
	remote := &isolationReviewRemoteDecision{}
	s.services.Backend.ConfigureWorkerInteractions(remote)
	body := map[string]any{"id": "remote-approval-web-operation", "args": []any{api.Decision{ID: "remote-original-request", Choice: "approve"}}}
	if w := c.request("/api/v1/conversation/decide", body); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	op := s.operations["remote-approval-web-operation"]
	if op.Error != "operation_unconfirmed" || remote.calls != 1 {
		t.Fatal("setup failed", op, remote.calls)
	}
	// A separate native local decision is healthy at the product handler. The
	// transport-layer test separately checks whether the actual Web client sends it.
	local := map[string]any{"id": "local-approval-web-operation", "args": []any{api.Decision{ID: "local-original-request", Choice: "approve"}}}
	if w := c.request("/api/v1/conversation/decide", local); w.Code != 200 || e.localDecisions != 1 {
		t.Fatal(w.Code, w.Body.String(), e.localDecisions)
	}
	if !s.UpdateQuiescent() {
		t.Fatal("completed unknown remote decision permanently blocks local managed update")
	}
}

func (e *isolationReviewRemoteDecision) RemoteWorkApproval(id string) bool {
	return id == "remote-original-request"
}
