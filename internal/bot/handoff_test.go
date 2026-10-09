package bot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestPrivateHandoffPersistsAcrossRestartAndConsumesOnlyAcceptedDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff-codex.json")
	h, err := openHandoff(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.save("call-1", "old-session", "Identity; goal; pending task original receipt."); err != nil {
		t.Fatal(err)
	}
	if err := h.save("call-2", "old-session", "overwrite"); err == nil {
		t.Fatal("overwrote unresolved original call")
	}
	h, err = openHandoff(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.prepare().Text != "" || h.prepareFor("old-session").Text != "" {
		t.Fatal("handoff entered the old context before new session receipt")
	}
	if err := h.record("call-1", "new-session", "committed"); err != nil {
		t.Fatal(err)
	}
	if h.prepareFor("old-session").Text != "" {
		t.Fatal("handoff replayed into old session")
	}
	seed := h.prepare()
	if !strings.Contains(seed.Text, "pending task original receipt") || seed.HandoffDigest == "" {
		t.Fatal("private handoff not restored")
	}
	if err := h.consume(api.ContextSeed{HandoffDigest: "different"}); err != nil {
		t.Fatal(err)
	}
	if h.prepare().HandoffDigest != seed.HandoffDigest {
		t.Fatal("wrong receipt consumed handoff")
	}
	if err := h.consume(seed); err != nil {
		t.Fatal(err)
	}
	h, err = openHandoff(path)
	if err != nil || h.prepare().Text != "" || len(h.state.Receipts) != 1 || h.state.Receipts[0].NewSession != "new-session" {
		t.Fatal("accepted handoff replayed", err)
	}
}

type blockedRenewEngine struct {
	fakeEngine
	entered chan struct{}
	release chan struct{}
	renewed bool
}

func (e *blockedRenewEngine) RenewAfterTool(ctx context.Context, invocation api.ToolInvocation) (string, error) {
	close(e.entered)
	select {
	case <-e.release:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	e.renewed = true
	return "new-session", nil
}

type reportAfterRenewal struct {
	engine *blockedRenewEngine
	calls  int
}

func (r *reportAfterRenewal) DeliverTaskReport(context.Context) error {
	if !r.engine.renewed {
		panic("task feedback entered old context before terminal handoff")
	}
	r.calls++
	return nil
}

func TestConcurrentUserAndFeedbackWaitForTerminalHandoff(t *testing.T) {
	r, _, _ := fixture(t)
	if err := r.openHandoff(); err != nil {
		t.Fatal(err)
	}
	if err := r.handoff.saveTurn("original-call", "old-session", "terminal-turn", "Identity; original task handle and receipt; next action."); err != nil {
		t.Fatal(err)
	}
	e := &blockedRenewEngine{fakeEngine: fakeEngine{outcome: "accepted"}, entered: make(chan struct{}), release: make(chan struct{})}
	reports := &reportAfterRenewal{engine: e}
	r.engine, r.reports = e, reports
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	tickDone := make(chan error, 1)
	go func() { tickDone <- r.Tick(ctx) }()
	select {
	case <-e.entered:
	case <-ctx.Done():
		t.Fatal("renewal not reached")
	}
	userDone := make(chan error, 1)
	go func() {
		_, err := r.SubmitUser(ctx, api.Submission{ID: "user-after-dream", Text: "Continue"}, nil)
		userDone <- err
	}()
	if len(e.submissions) != 0 || reports.calls != 0 {
		t.Fatal("input or feedback crossed pending handoff")
	}
	close(e.release)
	if err := <-tickDone; err != nil {
		t.Fatal(err)
	}
	if err := <-userDone; err != nil {
		t.Fatal(err)
	}
	if reports.calls != 1 || len(e.submissions) != 1 || e.submissions[0].ID != "user-after-dream" || r.handoff.pending().NewSession != "new-session" {
		t.Fatal("message or feedback lost after renewal", reports.calls, e.submissions, r.handoff.pending())
	}
}

func TestUserInputIsNotSubmittedIntoAnActiveTerminalTurn(t *testing.T) {
	r, _, _ := fixture(t)
	if err := r.openHandoff(); err != nil {
		t.Fatal(err)
	}
	if err := r.handoff.saveTurn("original-call", "old-session", "terminal-turn", "Identity; original task and receipt."); err != nil {
		t.Fatal(err)
	}
	e := &legacyDreamEngine{fakeEngine: fakeEngine{outcome: "accepted"}, state: api.ConversationState{Session: "old-session", Turn: "terminal-turn", Status: "running", Observed: true, Idle: false}}
	r.engine = e // no renewal port: simulate a terminal receipt still in progress
	receipt, err := r.SubmitUser(t.Context(), api.Submission{ID: "early-user", Text: "Continue"}, nil)
	if err != nil || receipt.Outcome != "rejected" || len(e.submissions) != 0 || r.handoff.pending().CallID != "original-call" {
		t.Fatal("user input entered a Turn that is still terminating", receipt, err, e.submissions)
	}
	e.state.Status, e.state.Idle = "completed", true
	receipt, err = r.SubmitUser(t.Context(), api.Submission{ID: "early-user", Text: "Continue"}, nil)
	if err != nil || receipt.Outcome != "accepted" || len(e.submissions) != 1 || r.handoff.pending().CallID != "" {
		t.Fatal("terminal fallback did not accept original user input", receipt, err, e.submissions)
	}
}

func TestDreamToolKeepsOriginalCallAndFencesBotEffectsThroughFirstInput(t *testing.T) {
	r, _, _ := fixture(t)
	if err := r.openHandoff(); err != nil {
		t.Fatal(err)
	}
	invocation := api.ToolInvocation{Provider: "codex", CallID: `"mcp-call-7"`, Session: "old-session", Turn: "original-turn"}
	ctx := api.WithToolInvocation(t.Context(), invocation)
	out := r.CallTool(ctx, "bot_dream", json.RawMessage(`{"handoff":"Identity; task handle task-123; unknown receipt receipt-456; next action."}`))
	if out.IsError || !out.TurnComplete || r.handoff.pending().CallID != invocation.CallID {
		t.Fatal("original terminal call not saved", out)
	}
	other := json.RawMessage(`{"request":{"type":"context"}}`)
	if accepted := r.CallTool(t.Context(), "bot_schedule", other); !accepted.IsError {
		t.Fatal("same-batch Bot effect crossed terminal call", accepted)
	}
	if err := r.handoff.record(invocation.CallID, "new-session", "committed"); err != nil {
		t.Fatal(err)
	}
	if accepted := r.CallTool(t.Context(), "bot_schedule", other); !accepted.IsError {
		t.Fatal("old-session Bot effect crossed native renewal", accepted)
	}
	seed := r.PrepareHandoffContextFor("new-session")
	if !strings.Contains(seed.Text, "receipt-456") || r.PrepareHandoffContextFor("old-session").Text != "" {
		t.Fatal("handoff session binding wrong", seed)
	}
	if err := r.ConsumeHandoffContext(seed); err != nil {
		t.Fatal(err)
	}
	if accepted := r.CallTool(t.Context(), "bot_schedule", other); accepted.IsError {
		t.Fatal("new-session Bot tool stayed fenced", accepted)
	}
	if duplicate := r.CallTool(ctx, "bot_dream", json.RawMessage(`{"handoff":"Identity; task handle task-123; unknown receipt receipt-456; next action."}`)); !duplicate.IsError || duplicate.TurnComplete {
		t.Fatal("consumed original call replayed a second handoff", duplicate)
	}
	r2, err := NewForRuntime(r.path, "codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r2.openHandoff(); err != nil {
		t.Fatal(err)
	}
	if r2.handoff.pending().CallID != "" || len(r2.handoff.pending().Receipts) != 1 || r2.handoff.pending().Receipts[0].CallID != invocation.CallID {
		t.Fatal("original receipt not durable across restart")
	}
}

func TestFailedDreamHandoffPreservesRecoverableSummaryAndOldContext(t *testing.T) {
	r, _, _ := fixture(t)
	if err := r.openHandoff(); err != nil {
		t.Fatal(err)
	}
	invocation := api.ToolInvocation{Provider: "codex", CallID: `"mcp-call-unknown"`, Session: "old-session", Turn: "original-turn"}
	if out := r.CallTool(api.WithToolInvocation(t.Context(), invocation), "bot_dream", json.RawMessage(`{"handoff":"Keep original task receipt-unknown."}`)); out.IsError || !out.TurnComplete {
		t.Fatal(out)
	}
	if err := r.handoff.record(invocation.CallID, "", "unknown_fallback"); err != nil {
		t.Fatal(err)
	}
	if r.handoff.prepare().Text != "" || r.handoff.pending().CallID != "" {
		t.Fatal("failed renewal contaminated old context")
	}
	if got := r.handoff.pending().Receipts[0]; got.CallID != invocation.CallID || got.Source != invocation.Session || got.Turn != invocation.Turn || got.Outcome != "unknown_fallback" || !strings.Contains(got.Text, "receipt-unknown") {
		t.Fatal("original unknown receipt or private summary lost", got)
	}
	if accepted := r.CallTool(t.Context(), "bot_schedule", json.RawMessage(`{"request":{"type":"context"}}`)); accepted.IsError {
		t.Fatal("old context became unusable", accepted)
	}
}

func TestHandoffReceiptWriteFailureKeepsCurrentProcessAndOriginalDiskRecoverable(t *testing.T) {
	for _, outcome := range []string{"committed", "fallback"} {
		t.Run(outcome, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "handoff.json")
			h, err := openHandoff(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.saveTurn("original-call", "old-session", "turn", "Identity; original receipt; next action."); err != nil {
				t.Fatal(err)
			}
			h.path = t.TempDir() // local write failure after the durable original
			newSession := ""
			if outcome == "committed" {
				newSession = "new-session"
			}
			if err := h.record("original-call", newSession, outcome); err == nil {
				t.Fatal("synthetic disk failure was not observed")
			}
			if outcome == "committed" && h.prepareFor("new-session").Text == "" {
				t.Fatal("accepted new Session cannot receive in-memory handoff")
			}
			if outcome == "fallback" && h.pending().CallID != "" {
				t.Fatal("old context stayed fenced after in-memory fallback")
			}
			restored, err := openHandoff(path)
			if err != nil || restored.pending().CallID != "original-call" || restored.pending().Source != "old-session" || restored.pending().Text == "" {
				t.Fatal("original on-disk handoff lost", err, restored)
			}
		})
	}
}
