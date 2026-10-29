package bot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

type guardedDreamEngine struct {
	legacyDreamEngine
	afterCreate        func()
	beforeTurnSnapshot func()
	creates            int
}

func (e *guardedDreamEngine) ConversationState() api.ConversationState {
	if e.beforeTurnSnapshot != nil {
		e.beforeTurnSnapshot()
	}
	return e.state
}

func TestHumanIngressFencesDreamWhileNativeTurnSnapshotIsBlocked(t *testing.T) {
	r, _, _ := fixture(t)
	if err := r.openHandoff(); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	e := &guardedDreamEngine{beforeTurnSnapshot: func() { close(entered); <-release }}
	r.engine = e
	done := make(chan func(), 1)
	go func() { finish, _ := r.BeginUserActivity(); done <- finish }()
	<-entered
	call := api.ToolInvocation{Provider: r.provider, CallID: "original", Session: "old", Turn: "turn"}
	out := r.CallTool(api.WithToolInvocation(t.Context(), call), "bot_dream", json.RawMessage(`{"handoff":"Original goal and its limits."}`))
	close(release)
	finish := <-done
	defer finish()
	if out.IsError || !r.handoff.pending().Superseded {
		t.Fatal("capture overtook human ingress registration", out)
	}
	if err := r.RenewPendingToolHandoff(t.Context()); err != nil || e.creates != 0 {
		t.Fatal("creation overtook the queued human", err)
	}
}

func TestHumanAlreadyWaitingBeforeDreamCaptureDefersOnlyItsOldTurn(t *testing.T) {
	r, _, _ := fixture(t)
	if err := r.openHandoff(); err != nil {
		t.Fatal(err)
	}
	e := &guardedDreamEngine{legacyDreamEngine: legacyDreamEngine{fakeEngine: fakeEngine{outcome: "accepted"}, state: api.ConversationState{Session: "old", Turn: "turn", Status: "completed", Observed: true, Idle: true}}}
	r.engine = e
	done, err := r.BeginUserActivity()
	if err != nil {
		t.Fatal(err)
	}
	call := api.ToolInvocation{Provider: r.provider, CallID: "original", Session: "old", Turn: "turn"}
	out := r.CallTool(api.WithToolInvocation(t.Context(), call), "bot_dream", json.RawMessage(`{"handoff":"Original goal and its limits."}`))
	if out.IsError || !r.handoff.pending().Superseded {
		t.Fatal("queued human correction preceded capture but was missed", out)
	}
	if err := r.RenewPendingToolHandoff(t.Context()); err != nil || e.creates != 0 || e.state.Session != "old" {
		t.Fatal("old goal was renewed", err)
	}
	done()
	e.state.Turn = "next-turn"
	call.CallID, call.Turn = "next-call", "next-turn"
	out = r.CallTool(api.WithToolInvocation(t.Context(), call), "bot_dream", json.RawMessage(`{"handoff":"Corrected goal and its original limits."}`))
	if out.IsError || r.handoff.pending().Superseded {
		t.Fatal("later turn inherited the earlier correction fence", out)
	}
}

func (e *guardedDreamEngine) RenewAfterTool(ctx context.Context, _ api.ToolInvocation) (string, error) {
	if err := api.CheckRenewalGuard(ctx); err != nil {
		return "", err
	}
	e.creates++
	if e.afterCreate != nil {
		e.afterCreate()
	}
	if err := api.CheckRenewalGuard(ctx); err != nil {
		return "", err
	}
	e.state.Session = "new-session"
	return e.state.Session, nil
}

func TestDreamReadsRequiredMemoryBeforeEndingTurn(t *testing.T) {
	r, _, _ := fixture(t)
	v, err := notebook.OpenVault(filepath.Join(t.TempDir(), "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err := r.ConfigureDream(v, ""); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(v.Path(), "MEMORY.md")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	ctx := api.WithToolInvocation(t.Context(), api.ToolInvocation{Provider: "codex", CallID: "original", Session: "old", Turn: "turn"})
	out := r.CallTool(ctx, "bot_dream", json.RawMessage(`{"handoff":"Keep task-original and authorization limits."}`))
	if !out.IsError || out.TurnComplete || r.handoff.pending().CallID != "" {
		t.Fatal("unreadable memory ended the owning turn", out)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Identity and stable preferences."), 0600); err != nil {
		t.Fatal(err)
	}
	out = r.CallTool(ctx, "bot_dream", json.RawMessage(`{"handoff":"Keep task-original and authorization limits."}`))
	if out.IsError || !out.TurnComplete || r.handoff.pending().MemoryDigest == "" {
		t.Fatal("repaired memory did not allow the original unaccepted call", out)
	}
}

func TestMemoryChangeDuringCreationKeepsOldConversation(t *testing.T) {
	for _, change := range []string{"unreadable", "edited", "deleted"} {
		t.Run(change, func(t *testing.T) {
			r, _, _ := fixture(t)
			v, err := notebook.OpenVault(filepath.Join(t.TempDir(), "Notebook"))
			if err != nil {
				t.Fatal(err)
			}
			defer v.Close()
			if err := r.ConfigureDream(v, ""); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(v.Path(), "MEMORY.md")
			if err := os.WriteFile(path, []byte("Identity; stable preference; original limit."), 0600); err != nil {
				t.Fatal(err)
			}
			e := &guardedDreamEngine{legacyDreamEngine: legacyDreamEngine{fakeEngine: fakeEngine{outcome: "accepted"}, state: api.ConversationState{Session: "old", Turn: "turn", Status: "completed", Observed: true, Idle: true}}}
			r.engine = e
			e.afterCreate = func() {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				switch change {
				case "unreadable":
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				case "edited":
					if err := os.WriteFile(path, []byte("A new explicit preference and narrower limit."), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			invocation := api.ToolInvocation{Provider: "codex", CallID: "original", Session: "old", Turn: "turn"}
			out := r.CallTool(api.WithToolInvocation(t.Context(), invocation), "bot_dream", json.RawMessage(`{"handoff":"Original goal; task-original; pending question; revoked scope; uncertain operation receipt-original."}`))
			if out.IsError || !out.TurnComplete {
				t.Fatal(out)
			}
			if err := r.RenewPendingToolHandoff(t.Context()); err != nil {
				t.Fatal(err)
			}
			pending := r.handoff.pending()
			if e.creates != 1 || e.state.Session != "old" || pending.CallID != "" || len(pending.Receipts) != 1 || pending.Receipts[0].Outcome != "fallback" || pending.Receipts[0].CallID != "original" || pending.Receipts[0].Text == "" {
				t.Fatal("changed memory lost the source or original receipt", e, pending)
			}
			if receipt, err := r.SubmitUser(t.Context(), api.Submission{ID: "correction", Text: "Continue with the corrected limit."}, nil); err != nil || receipt.Outcome != "accepted" || e.state.Session != "old" {
				t.Fatal("old conversation stopped accepting original input", receipt, err)
			}
		})
	}
}

func TestHumanStopDefersHandoffAcrossRestart(t *testing.T) {
	r, _, _ := fixture(t)
	if err := r.openHandoff(); err != nil {
		t.Fatal(err)
	}
	if err := r.handoff.saveTurn("original", "old", "turn", "Keep original receipt."); err != nil {
		t.Fatal(err)
	}
	done, err := r.BeginUserActivity()
	done()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := NewForRuntime(r.path, r.provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.openHandoff(); err != nil {
		t.Fatal(err)
	}
	e := &guardedDreamEngine{legacyDreamEngine: legacyDreamEngine{state: api.ConversationState{Session: "old", Turn: "turn", Status: "completed", Observed: true, Idle: true}}}
	restored.engine = e
	if err := restored.RenewPendingToolHandoff(t.Context()); err != nil || e.creates != 0 || e.state.Session != "old" {
		t.Fatal("restart ignored persisted human priority", err, e.creates)
	}
}
