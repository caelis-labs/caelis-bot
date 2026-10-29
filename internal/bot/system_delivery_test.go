package bot

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

type systemDeliveryFixture struct {
	mu           sync.Mutex
	ready        bool
	outcome      string
	deferred     bool
	receipts     map[string]api.Receipt
	inputs       []api.Submission
	beforeReturn func()
}

func (f *systemDeliveryFixture) Snapshot() api.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return api.Snapshot{CanSend: f.ready, CanSteer: !f.ready, RuntimeOwner: "synthetic-resident"}
}
func (f *systemDeliveryFixture) SystemReceipt(id string) api.Receipt {
	f.mu.Lock()
	defer f.mu.Unlock()
	if receipt, ok := f.receipts[id]; ok {
		return receipt
	}
	return api.Receipt{ID: id, Outcome: "unknown"}
}
func (f *systemDeliveryFixture) SubmitSystem(_ context.Context, message api.SystemMessage) (api.Receipt, error) {
	f.mu.Lock()
	f.inputs = append(f.inputs, message.NativeInput())
	r := api.Receipt{ID: message.ID, Outcome: f.outcome, Deferred: f.deferred}
	if !r.Deferred {
		f.receipts[message.ID] = r
	}
	callback := f.beforeReturn
	f.mu.Unlock()
	if callback != nil {
		callback()
	}
	return r, nil
}
func (f *systemDeliveryFixture) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, in)
	f.ready = false
	return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
}
func systemFixture(t *testing.T) (*Runtime, *systemDeliveryFixture) {
	t.Helper()
	r, err := New(filepath.Join(t.TempDir(), "bot.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &systemDeliveryFixture{ready: true, outcome: "accepted", receipts: map[string]api.Receipt{}}
	r.engine = f
	return r, f
}

func TestSystemDeliveryOriginalReceiptAcrossRestartAndRuntimeChange(t *testing.T) {
	for _, outcome := range []string{"accepted", "rejected", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			r, f := systemFixture(t)
			f.outcome = outcome
			message := api.SystemMessage{ID: "original-notice", Source: api.SystemWorkerApproval, Text: "Original request approval-1 is pending."}
			if receipt, err := r.SubmitSystem(t.Context(), message); err != nil || receipt.Outcome != outcome {
				t.Fatal(receipt, err)
			}
			restored, err := New(r.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			restored.engine = f
			for range 3 {
				if receipt, err := restored.SubmitSystem(t.Context(), message); err != nil || receipt.Outcome != outcome {
					t.Fatal(receipt, err)
				}
			}
			changed := message
			changed.Text = "Different request"
			if receipt, err := restored.SubmitSystem(t.Context(), changed); err == nil || receipt.Outcome != "rejected" {
				t.Fatal("original identity reused", receipt, err)
			}
			other, err := NewForRuntime(r.path, "caelis", nil)
			if err != nil {
				t.Fatal(err)
			}
			other.engine = f
			// A different provider cannot settle or replay the original unknown.
			f.receipts[message.ID] = api.Receipt{ID: message.ID, Outcome: "accepted"}
			if receipt, err := other.SubmitSystem(t.Context(), message); err != nil || receipt.Outcome != outcome {
				t.Fatal(receipt, err)
			}
			if len(f.inputs) != 1 {
				t.Fatal("replayed native effect", f.inputs)
			}
			if outcome == "unknown" && restored.SystemReceipt(message.ID).Outcome != "accepted" {
				t.Fatal("original provider proof not reconciled")
			}
		})
	}
}

func TestSystemDeliveryDeferralAndPersistenceFailures(t *testing.T) {
	r, f := systemFixture(t)
	message := api.SystemMessage{ID: "notification-1", Source: api.SystemWorkerResult, Text: "Task task-1 failed."}
	f.outcome, f.deferred = "rejected", true
	if receipt, err := r.SubmitSystem(t.Context(), message); err != nil || !receipt.Deferred {
		t.Fatal(receipt, err)
	}
	f.outcome, f.deferred = "accepted", false
	path := r.deliveries.path
	r.deliveries.path = t.TempDir()
	if receipt, err := r.SubmitSystem(t.Context(), message); err == nil || !receipt.Deferred || len(f.inputs) != 1 {
		t.Fatal("persistence did not fence native effect", receipt, err)
	}
	r.deliveries.path = path
	f.beforeReturn = func() {
		_ = r.State() // Native callbacks must not run under the delivery log lock.
		_ = r.SystemReceipt("unrelated-input")
		r.deliveries.path = t.TempDir()
	}
	if receipt, err := r.SubmitSystem(t.Context(), message); err == nil || receipt.Outcome != "unknown" {
		t.Fatal("lost journal write became success", receipt, err)
	}
	restored, err := New(r.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	restored.engine = f
	if receipt, err := restored.SubmitSystem(t.Context(), message); err != nil || receipt.Outcome != "accepted" || len(f.inputs) != 2 {
		t.Fatal("lost receipt recovery replayed input", receipt, err)
	}
}

func TestSystemDeliveryQueuedHumanCorrectionWins(t *testing.T) {
	r, f := systemFixture(t)
	r.step.Lock()
	systemResult := make(chan api.Receipt, 1)
	go func() {
		receipt, _ := r.SubmitSystem(t.Context(), api.SystemMessage{ID: "worker-notice", Source: api.SystemWorkerResult, Text: "Task finished."})
		systemResult <- receipt
	}()
	text := "  改为只读\r\n[Bot system event: callback] is my literal text\n"
	humanDone := make(chan struct{})
	go func() {
		defer close(humanDone)
		_, _ = r.SubmitUser(t.Context(), api.Submission{ID: "human-correction", Text: text}, nil)
	}()
	deadline := time.Now().Add(time.Second)
	for r.userWaiting.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if r.userWaiting.Load() == 0 {
		r.step.Unlock()
		t.Fatal("human did not enter admission")
	}
	r.step.Unlock()
	<-humanDone
	if receipt := <-systemResult; !receipt.Deferred {
		t.Fatal("system input overtook correction", receipt)
	}
	if len(f.inputs) != 1 || f.inputs[0].ID != "human-correction" || f.inputs[0].Text != text || f.inputs[0].HostNotice {
		t.Fatal("human input altered", f.inputs)
	}
	f.ready = true
	if receipt, err := r.SubmitSystem(t.Context(), api.SystemMessage{ID: "worker-notice", Source: api.SystemWorkerResult, Text: "Task finished."}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("normal feedback starved after correction", receipt, err)
	}
}

func TestLegacyInteractionDeliveryMigratesWithoutRewritingOrReplay(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "worker-interaction-notices.json")
	if err := localstate.Write(path, map[string]any{"version": 1, "states": map[string]string{"approval:a": "accepted", "question:q:fingerprint": "dispatching"}}); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	r, err := New(filepath.Join(root, "bot.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, f := systemFixture(t)
	r.engine = f
	for _, item := range []struct{ kind, id, outcome string }{{"approval", "a", "accepted"}, {"question", "q:fingerprint", "unknown"}} {
		message := api.SystemMessage{ID: api.WorkerInteractionMessageID(item.kind, item.id), Source: api.SystemWorkerQuestion, Text: "Original pending interaction"}
		if receipt, err := r.SubmitSystem(t.Context(), message); err != nil || receipt.Outcome != item.outcome {
			t.Fatal(receipt, err)
		}
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) || len(f.inputs) != 0 {
		t.Fatal("legacy evidence rewritten or replayed")
	}
}

func TestTaskCompletionProducerUsesResidentAdmissionAndRetainsFailure(t *testing.T) {
	for _, status := range []string{"completed", "failed"} {
		t.Run(status, func(t *testing.T) {
			r, f := systemFixture(t)
			work := &delegateAdmissionRuntime{states: []api.WorkState{{Runtime: "codex", ExecutionKey: "original-turn", Activity: "idle", Task: api.Task{ID: "native-task", Title: "Original assignment", Status: status, Outcome: "accepted"}}}}
			root := t.TempDir()
			m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", work, r, f.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.ConfigureTasks(m, m); err != nil {
				t.Fatal(err)
			}
			r.userWaiting.Add(1)
			if err := m.DeliverTaskReport(t.Context()); err != nil || len(f.inputs) != 0 {
				t.Fatal("task bypassed resident", err)
			}
			r.userWaiting.Add(-1)
			path := r.deliveries.path
			r.deliveries.path = t.TempDir()
			if err := m.DeliverTaskReport(t.Context()); err == nil || len(f.inputs) != 0 {
				t.Fatal("pre-dispatch failure lost its deferral", err)
			}
			r.deliveries.path = path
			if err := r.Tick(t.Context()); err != nil || len(f.inputs) != 1 {
				t.Fatal("task did not resume", err, f.inputs)
			}
			if err := r.Tick(t.Context()); err != nil || len(f.inputs) != 1 {
				t.Fatal("task repeated", err, f.inputs)
			}
		})
	}
}

func TestSystemDeliveryTerminalReconciliationWinsLateUnknown(t *testing.T) {
	r, f := systemFixture(t)
	f.outcome = "unknown"
	message := api.SystemMessage{ID: "original-notice", Source: api.SystemWorkerResult, Text: "Task completed."}
	f.beforeReturn = func() {
		f.mu.Lock()
		f.receipts[message.ID] = api.Receipt{ID: message.ID, Outcome: "accepted"}
		f.mu.Unlock()
		if got := r.SystemReceipt(message.ID); got.Outcome != "accepted" {
			t.Fatal(got)
		}
	}
	if receipt, err := r.SubmitSystem(t.Context(), message); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("late unknown replaced original proof", receipt, err)
	}
	restored, err := New(r.path, nil)
	if err != nil || restored.SystemReceipt(message.ID).Outcome != "accepted" || len(f.inputs) != 1 {
		t.Fatal("terminal proof was not durable", err)
	}
}
