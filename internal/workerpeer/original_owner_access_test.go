package workerpeer

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
	"time"
)

func auditServer(t *testing.T, o Owner) (*Server, context.Context) {
	t.Helper()
	s, e := Open(t.TempDir(), func(context.Context, string, string, string) (Owner, string, *api.WorkExecutionSettings, error) {
		return o, "codex", nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	s.state.Grants["source-a"] = grant{Peer: "source-a", Hash: tokenHash("proof")}
	s.state.Bindings["source-a"] = map[string]string{"task-a": "codex"}
	ctx := context.WithValue(t.Context(), authenticatedGrantKey{}, authenticatedGrant{Peer: "source-a", Hash: tokenHash("proof")})
	return s, ctx
}
func TestAuditOriginalOperationReceiptWithoutNativeConnect(t *testing.T) {
	o := newFixtureOwner()
	q := api.WorkOperationQuery{TaskID: "task-a", RequestID: "original-op", Kind: "send", Fingerprint: "original-digest"}
	o.ops[q.RequestID] = api.WorkOperation{WorkOperationQuery: q, Outcome: "accepted"}
	o.unavailable = true
	s, ctx := auditServer(t, o)
	out := s.dispatch(ctx, "source-a", Request{Action: "operation", Query: q})
	if out.Error != "" || out.Operation.Outcome != "accepted" {
		t.Fatalf("persisted original accepted receipt hidden after owner exit: error=%q outcome=%q", out.Error, out.Operation.Outcome)
	}
}

type auditStopOwner struct {
	*fixtureOwner
	waiting bool
	stops   int
}

func (o *auditStopOwner) Connect(ctx context.Context) error {
	if o.waiting {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}
func (o *auditStopOwner) StopWorkRequest(ctx context.Context, in api.WorkStop) (api.Task, error) {
	o.stops++
	o.waiting = false
	return api.Task{ID: in.ID, Status: "interrupted", Outcome: "accepted"}, nil
}
func TestAuditStopDoesNotWaitBehindNativeConnect(t *testing.T) {
	o := &auditStopOwner{fixtureOwner: newFixtureOwner(), waiting: true}
	s, ctx := auditServer(t, o)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	out := s.dispatch(ctx, "source-a", Request{Action: "stop", Stop: api.WorkStop{ID: "task-a", RequestID: "original-stop", ExecutionKey: "owner-thread-run"}})
	if out.Error != "" || o.stops != 1 {
		t.Fatalf("guarded stop never reaches native pre-lock cancellation: error=%q stop effects=%d", out.Error, o.stops)
	}
}
