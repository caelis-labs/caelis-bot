package bot

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localipc"
)

type budgetTasks struct {
	compactTasks
	wait    time.Duration
	entered chan string
	ended   chan struct{}
	calls   atomic.Int32
}

func (f *budgetTasks) StartTask(ctx context.Context, in api.TaskStart) (api.Task, error) {
	f.calls.Add(1)
	if f.entered != nil {
		f.entered <- in.RequestID
	}
	if f.ended != nil {
		defer close(f.ended)
	}
	timer := time.NewTimer(f.wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return api.Task{ID: "original-worker", Outcome: "unknown", Status: "unknown"}, ctx.Err()
	case <-timer.C:
		return api.Task{ID: "original-worker", Outcome: "accepted", Status: "working"}, nil
	}
}
func fileBudgetBridge(t *testing.T, f *budgetTasks) (*Runtime, *Bridge) {
	t.Helper()
	r, _, _ := fixture(t)
	r.engine = &legacyDreamEngine{state: api.ConversationState{Session: "resident", Turn: "original-turn", TurnKey: "original-key"}}
	r.ConfigureTasks(f, nil)
	b, err := Serve(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return r, b
}
func fileBudgetRequest(b *Bridge) toolRequest {
	return toolRequest{Token: b.token, ID: json.RawMessage(`"original-file-call"`), Version: 2, Name: "bot_delegate", Arguments: json.RawMessage(`{"prompt":"read selected input","attachments":[{"id":"owned-upload"}]}`)}
}
func TestFileDelegationBudgetKeepsNestedLimitsConsistent(t *testing.T) {
	file := json.RawMessage(`{"attachments":[{"id":"owned"}]}`)
	if api.WorkerAttachmentTimeout <= 25*time.Second || delegationBridgeTimeout(file) <= api.WorkerAttachmentTimeout || api.WorkerMCPTimeout <= delegationBridgeTimeout(file) {
		t.Fatal("a shorter wrapper still truncates file admission")
	}
	if delegationBridgeTimeout(json.RawMessage(`{}`)) != api.WorkerAdmissionTimeout+10*time.Second {
		t.Fatal("plain delegation budget changed")
	}
	// 160 MiB -> 214 MiB of JSON base64 allowance + the original 25s control path.
	if api.WorkerAttachmentTimeout != 239*time.Second {
		t.Fatal("transfer budget no longer matches the existing input cap", api.WorkerAttachmentTimeout)
	}
}
func TestFileDelegationRespectsOriginalCallerDeadline(t *testing.T) {
	r, _, _ := fixture(t)
	f := &budgetTasks{wait: time.Hour}
	r.ConfigureTasks(f, nil)
	parent, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	invocation := api.ToolInvocation{Provider: "codex", Session: "resident", Turn: "original", CallID: "cancel-original"}
	out := r.CallTool(api.WithToolInvocation(parent, invocation), "bot_delegate", json.RawMessage(`{"prompt":"read input","attachments":[{"id":"owned-upload"}]}`))
	data := out.StructuredContent["data"].(map[string]any)
	if !out.IsError || data["requestId"] != toolSubmissionID(invocation) || data["id"] != "original-worker" || f.calls.Load() != 1 || out.StructuredContent["outcome"] != "unknown" {
		t.Fatal("cancellation lost original uncertain receipt", out.StructuredContent)
	}
}
func TestFileDelegationBridgeCancellationStopsPreparation(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "caller-loss", true: "bridge-close"}[shutdown], func(t *testing.T) {
			f := &budgetTasks{wait: time.Hour, entered: make(chan string, 1), ended: make(chan struct{})}
			_, b := fileBudgetBridge(t, f)
			conn, err := localipc.Dial(b.listener.Endpoint(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err = json.NewEncoder(conn).Encode(fileBudgetRequest(b)); err != nil {
				t.Fatal(err)
			}
			select {
			case <-f.entered:
			case <-time.After(time.Second):
				t.Fatal("delegation never entered")
			}
			if shutdown {
				go b.Close()
			} else {
				_ = conn.Close()
			}
			select {
			case <-f.ended:
			case <-time.After(time.Second):
				t.Fatal("lost caller/shutdown left file preparation running")
			}
			if f.calls.Load() != 1 {
				t.Fatal("cancellation replayed original invocation")
			}
		})
	}
}
func TestFileDelegationBridgePassesFormer25SecondAdmissionLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("26s real bridge deadline regression")
	}
	f := &budgetTasks{wait: 26 * time.Second}
	r, b := fileBudgetBridge(t, f)
	req := fileBudgetRequest(b)
	out, unknown := forwardOnce(b.listener.Endpoint(), req)
	if unknown || out.IsError || f.calls.Load() != 1 {
		t.Fatal("slow original file admission was truncated or replayed", unknown, out.StructuredContent)
	}
	expected := toolSubmissionID(api.ToolInvocation{Provider: r.provider, Session: "resident", Turn: "original-turn", TurnKey: "original-key", CallID: string(req.ID)})
	if out.StructuredContent["data"].(map[string]any)["requestId"] != expected {
		t.Fatal("slow transfer changed original operation")
	}
}
