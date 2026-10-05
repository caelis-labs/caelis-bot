package desktopcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/host"
	"github.com/caelis-labs/desktop-world/protocol"
)

type fakeClient struct {
	mu                  sync.Mutex
	calls, grants, ends int
	last                map[string]any
	block               chan struct{}
	entered             chan struct{}
	uncertain           bool
	reply               host.Reply
	declarations        []host.ApplicationGrant
}

func (*fakeClient) BeginTurn(context.Context, string) error { return nil }
func (f *fakeClient) EndTurn(ctx context.Context, _ string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	f.mu.Lock()
	f.ends++
	f.mu.Unlock()
	return nil
}
func (f *fakeClient) Grant(context.Context, string, dw.Ref) error {
	f.mu.Lock()
	f.grants++
	f.declarations = append(f.declarations, host.ApplicationGrant{ID: "grant-ref", Application: "app-1", State: "active"})
	f.mu.Unlock()
	return nil
}
func (f *fakeClient) Declare(_ context.Context, _, name, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.declarations = append(f.declarations, host.ApplicationGrant{ID: "grant-pending", Name: name, WindowTitle: title, State: "pending", Reason: "application_not_running"})
	return nil
}
func (f *fakeClient) Revoke(_ context.Context, _ string, app dw.Ref) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.declarations {
		if f.declarations[i].Application == app {
			f.declarations[i].State = "revoked"
		}
	}
	return nil
}
func (f *fakeClient) RevokeGrant(_ context.Context, _, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.declarations {
		if f.declarations[i].ID == id {
			f.declarations[i].State = "revoked"
		}
	}
	return nil
}
func (f *fakeClient) Grants(_ context.Context, turn string) (host.GrantStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return host.GrantStatus{Turn: turn, Grants: append([]host.ApplicationGrant(nil), f.declarations...)}, nil
}
func (f *fakeClient) Call(ctx context.Context, _, _, op string, args any) (host.Reply, error) {
	f.mu.Lock()
	f.calls++
	f.last = args.(map[string]any)
	f.mu.Unlock()
	if op == "act" && f.block != nil {
		close(f.entered)
		select {
		case <-f.block:
		case <-ctx.Done():
			return host.Reply{}, ctx.Err()
		}
	}
	if op == "act" && f.uncertain {
		return host.Reply{}, errors.New("transport interrupted")
	}
	if op == "observe" {
		name := "Fixture"
		b, _ := protocol.Marshal(dw.Observation{Epoch: "epoch", Objects: []dw.Object{{Ref: "app-1", Kind: dw.KindApplication, Lifecycle: dw.LifeLive, Name: dw.Fact[string]{Status: dw.FactKnown, Value: &name}}}})
		return host.Reply{Result: b}, nil
	}
	return f.reply, nil
}
func (f *fakeClient) Reconcile(context.Context, string, string) (host.Reply, error) {
	return f.reply, nil
}
func (*fakeClient) Close() {}
func fixtureController(t *testing.T) (*Controller, *fakeClient, context.Context) {
	t.Helper()
	f := &fakeClient{reply: host.Reply{Result: json.RawMessage(`{"run_id":"run-1","outcome":"completed","seat_health":"ready"}`)}}
	c := New("", t.TempDir())
	c.start = func(context.Context) (client, dw.Epoch, error) { return f, "epoch", nil }
	t.Cleanup(c.Close)
	ctx := WithTurn(t.Context(), "turn-one")
	if r := c.CallTool(ctx, Prefix+"observe", json.RawMessage(`{"requestId":"observe-1","args":{"scope":{"desktop":true}}}`)); r.IsError {
		t.Fatal(r)
	}
	return c, f, ctx
}
func TestExactObservedGrantAndHostOnlyEnvelope(t *testing.T) {
	c, f, ctx := fixtureController(t)
	for _, raw := range []string{
		`{"application":"app-1","name":"Wrong","purpose":"task"}`,
		`{"application":"window","name":"Fixture","purpose":"task"}`,
		`{"application":"app-1","name":"Fixture","purpose":"task","turn":"forged"}`,
		`{"application":"app-1","name":"Fixture","purpose":"task","name":"Wrong"}`,
	} {
		if !c.CallTool(ctx, Prefix+"authorize", json.RawMessage(raw)).IsError {
			t.Fatal("accepted unobserved/forged grant")
		}
	}
	if f.grants != 0 {
		t.Fatal("invalid grant dispatched")
	}
	if c.CallTool(ctx, Prefix+"authorize", json.RawMessage(`{"application":"app-1","name":"Fixture","purpose":"task"}`)).IsError || f.grants != 1 {
		t.Fatal("valid reviewed grant rejected")
	}
	for _, name := range ApprovedTools() {
		if name == Prefix+"authorize" {
			t.Fatal("grant auto-approved")
		}
	}
}
func TestRequestDedupConflictAndRecoveryAcrossTurns(t *testing.T) {
	c, f, ctx := fixtureController(t)
	raw := json.RawMessage(`{"requestId":"action-1","args":{"steps":[{"id":"one","op":"invoke","target":{"ref":"button"}}]}}`)
	if c.CallTool(ctx, Prefix+"act", raw).IsError {
		t.Fatal("action rejected")
	}
	if f.last["epoch"] != nil || f.last["request_id"] != nil {
		t.Fatal("helper-owned identity was supplied as plan arguments")
	}

	c.EndTurn("turn-one")
	if c.CallTool(WithTurn(t.Context(), "turn-two"), Prefix+"act", raw).IsError || f.calls != 2 {
		t.Fatal("retry replayed or lost original receipt")
	}
	if !c.CallTool(ctx, Prefix+"act", json.RawMessage(`{"requestId":"action-1","args":{"steps":[]}}`)).IsError || f.calls != 2 {
		t.Fatal("conflicting ID accepted")
	}
	if !c.CallTool(ctx, Prefix+"observe", json.RawMessage(`{"requestId":"observe-2","args":{"scope":{"desktop":true}}}`)).IsError {
		t.Fatal("ended turn resurrected")
	}
}
func TestControlRevocationDoesNotWaitForDataCall(t *testing.T) {
	c, f, ctx := fixtureController(t)
	f.block = make(chan struct{})
	f.entered = make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.CallTool(ctx, Prefix+"act", json.RawMessage(`{"requestId":"blocking-action","args":{"steps":[]}}`))
	}()
	<-f.entered
	stopped := make(chan struct{})
	go func() { c.EndTurn("turn-one"); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("control queued behind data")
	}
	close(f.block)
	<-done
	if f.ends != 1 {
		t.Fatal("revocation not sent")
	}
}
func TestUnknownResultRetainsReceiptAndNeverRestarts(t *testing.T) {
	c, f, ctx := fixtureController(t)
	f.uncertain = true
	r := c.CallTool(ctx, Prefix+"act", json.RawMessage(`{"requestId":"unknown-action","args":{"steps":[]}}`))
	if !r.IsError || f.ends == 0 {
		t.Fatal("uncertain input did not revoke turn")
	}
	r = c.CallTool(t.Context(), Prefix+"reconcile", json.RawMessage(`{"requestId":"unknown-action"}`))
	if r.IsError || !strings.Contains(r.Content[0]["text"], "run-1") || f.calls != 2 {
		t.Fatal("original receipt lost or mutation repeated")
	}
}
func TestModelBudgetPreservesReceiptAndDoesNotCapture(t *testing.T) {
	c, f, ctx := fixtureController(t)
	b, _ := json.Marshal(map[string]any{"run_id": "run-large", "outcome": "unknown", "seat_health": "fenced", "detail": strings.Repeat("x", 40000)})
	f.reply = host.Reply{Result: b}
	r := c.CallTool(ctx, Prefix+"act", json.RawMessage(`{"requestId":"large-action","args":{"steps":[]}}`))
	encoded, _ := json.Marshal(r)
	if len(encoded) > 32<<10 || !r.IsError || !strings.Contains(r.Content[0]["text"], "run-large") {
		t.Fatal("budget discarded receipt")
	}
	if len(r.Content) != 1 || r.Content[0]["type"] != "text" {
		t.Fatal("action captured image")
	}
}

func TestRunStatusKeepsOriginalActReceiptAfterTurn(t *testing.T) {
	c, f, ctx := fixtureController(t)
	out := c.CallTool(ctx, Prefix+"act", json.RawMessage(`{"requestId":"original-run-action","args":{"steps":[{"id":"invoke","op":"invoke","target":{"ref":"button"}}]}}`))
	if out.IsError {
		t.Fatal(out)
	}
	c.CallTool(ctx, Prefix+"cancel", json.RawMessage(`{"requestId":"cancel-original-run","args":{"run_id":"run-1"}}`))
	c.EndTurn("turn-one")
	before := f.calls
	got := c.ReadRun(t.Context(), "run-1")
	if got.IsError || !strings.Contains(got.Content[0]["text"], "original-run-action") || f.calls != before {
		t.Fatal("run status returned another request or dispatched", got)
	}
	if !c.ReadRun(t.Context(), "foreign-run").IsError || f.calls != before {
		t.Fatal("unknown run adopted")
	}
}
