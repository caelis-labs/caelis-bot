package bot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	if !out.IsError || out.TurnComplete || r.handoff.pending().CallID != "" {
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
	if !out.IsError || out.TurnComplete || r.handoff.pending().CallID != "" {
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
	err := api.CommitRenewal(ctx, "new-session", func() error {
		e.state.Session = "new-session"
		return nil
	})
	return e.state.Session, err
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

type committedHandoffEngine struct {
	legacyDreamEngine
	resident   *Runtime
	reconciled int
	seed       api.ContextSeed
}

func (e *committedHandoffEngine) RenewAfterTool(_ context.Context, call api.ToolInvocation) (string, error) {
	if call.CallID != "original-call" || call.Session != "old" {
		return "", errors.New("lost original create receipt")
	}
	e.reconciled++
	return "new", nil // Native binding committed before the host receipt was saved.
}
func (e *committedHandoffEngine) Submit(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
	e.seed = e.resident.PrepareHandoffContext()
	receipt, err := e.fakeEngine.Submit(ctx, in, files)
	if err == nil && receipt.Outcome == "accepted" {
		err = e.resident.ConsumeHandoffContext(e.seed)
	}
	return receipt, err
}
func (e *committedHandoffEngine) SubmitSystem(ctx context.Context, in api.SystemMessage) (api.Receipt, error) {
	return e.Submit(ctx, in.NativeInput(), nil)
}
func (e *committedHandoffEngine) SystemReceipt(id string) api.Receipt { return api.Receipt{ID: id} }
func TestRestartReconcilesCommittedNativeBindingBeforeFirstInput(t *testing.T) {
	for _, source := range []string{"human", "worker"} {
		t.Run(source, func(t *testing.T) {
			before, _, _ := fixture(t)
			if err := before.openHandoff(); err != nil {
				t.Fatal(err)
			}
			if err := before.handoff.saveTurn("original-call", "old", "terminal", "Task-123 is pending; Notebook/review.md lines 30 onward unread; original authorization is read-only."); err != nil {
				t.Fatal(err)
			}
			r, err := New(before.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = r.openHandoff(); err != nil {
				t.Fatal(err)
			}
			e := &committedHandoffEngine{resident: r, legacyDreamEngine: legacyDreamEngine{fakeEngine: fakeEngine{view: api.Snapshot{CanSend: true}, outcome: "accepted"}, state: api.ConversationState{Session: "new", Observed: true, Idle: true}}}
			r.engine = e
			var receipt api.Receipt
			if source == "human" {
				receipt, err = r.SubmitUser(t.Context(), api.Submission{ID: "first", Text: "Continue the review under the original limits."}, nil)
			} else {
				message := api.SystemMessage{ID: "first", Source: api.SystemWorkerQuestion, Text: "Original Worker question remains pending."}
				if first, err := r.SubmitSystem(t.Context(), message); err != nil || first.Outcome != "rejected" || !first.Deferred || len(e.submissions) != 0 {
					t.Fatal("system input stole the unrecovered seed", first, err)
				}
				if err = r.RenewPendingToolHandoff(t.Context()); err != nil {
					t.Fatal(err)
				}
				receipt, err = r.SubmitSystem(t.Context(), message)
			}
			if err != nil || receipt.Outcome != "accepted" || e.reconciled != 1 || len(e.submissions) != 1 || e.seed.HandoffDigest == "" || !strings.Contains(e.seed.Text, "Notebook/review.md lines 30") || r.handoff.pending().CallID != "" {
				t.Fatal("crash recovery lost or replayed the original handoff", receipt, err, e.reconciled, e.seed)
			}
		})
	}
}
