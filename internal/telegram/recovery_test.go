package telegram

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	tg "github.com/mymmrac/telego"
)

func TestDurableCallbackReloadRetainsOriginalIdentityWithoutFrameSizeGate(t *testing.T) {
	root := t.TempDir()
	query := recoveryQuery("original-callback", "original-native-choice", 31)
	query.Message.(*tg.Message).Date = 1
	doc := document{Version: 1, Inputs: map[string]string{"original-decision": "unknown"}, Messages: map[string]delivery{}, Ingress: []tg.Update{{UpdateID: 17, CallbackQuery: query}}}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	// Valid private metadata is independent of any Runtime wire-frame limit.
	body = append(body, []byte(strings.Repeat(" ", 9<<20))...)
	if err := os.WriteFile(filepath.Join(root, "telegram.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	b, err := Open(root, Host{})
	if err != nil || len(b.state.Ingress) != 1 || b.state.Inputs["original-decision"] != "unknown" {
		t.Fatal("durable control update lost", err)
	}
	u := b.state.Ingress[0]
	if ingressKind(u) != 0 || u.CallbackQuery.ID != "original-callback" || u.CallbackQuery.Data != "original-native-choice" || u.CallbackQuery.Message.(*tg.Message).MessageID != 31 {
		t.Fatal("callback identity changed")
	}
}

func recoveryQuery(id, data string, messageID int) *tg.CallbackQuery {
	return &tg.CallbackQuery{ID: id, Data: data, From: tg.User{ID: 20}, Message: &tg.Message{
		MessageID: messageID, From: &tg.User{ID: 123, IsBot: true}, Chat: tg.Chat{ID: 10, Type: "private"},
	}}
}

// This fixture uses the same Host -> Control -> native submission wiring as
// app.New, while the Telegram transport and native owner are deterministic.
type ingressRecoveryEngine struct {
	api.Engine
	mu         sync.Mutex
	snapshot   api.Snapshot
	recovery   api.RecoveryState
	submits    []api.Submission
	refuseOnce bool
}

func (e *ingressRecoveryEngine) Snapshot() api.Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshot
}
func (e *ingressRecoveryEngine) RecoveryState() api.RecoveryState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.recovery
}
func (e *ingressRecoveryEngine) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.recovery.Automatic || e.recovery.InProgress || in.NativeIngressFence != e.recovery.Fence {
		return api.Receipt{}, api.ErrRecoveryPending
	}
	if e.refuseOnce {
		e.refuseOnce = false
		e.recovery.Automatic = true
		return api.Receipt{}, api.ErrRecoveryPending
	}
	e.submits = append(e.submits, in)
	return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
}

func TestRecoveryRacePersistsOriginalUpdateAcrossBridgeRestart(t *testing.T) {
	e := &ingressRecoveryEngine{snapshot: api.Snapshot{Connection: "ready", CanSend: true}, recovery: api.RecoveryState{Fence: "owner:9"}, refuseOnce: true}
	s := backend.NewService(e, nil, nil, nil, nil)
	h := Host{Snapshot: s.Snapshot, Recovery: s.RecoveryState, Submit: func(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
		return backend.SubmitRemote(ctx, s, in, files)
	}}
	b, f := testBridge(t, h)
	paired(b)
	b.mu.Lock()
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatal(err)
	}
	b.mu.Unlock()
	f.updates <- []tg.Update{message(1, 10, 20, "original input")}
	b.launch(f)
	deadline := time.Now().Add(2 * time.Second)
	var state string
	var offset int
	for time.Now().Before(deadline) {
		b.mu.Lock()
		state, offset = b.state.Inputs["telegram:123:1"], b.state.Offset
		b.mu.Unlock()
		if state == "deferred" && offset == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if state != "deferred" || offset != 2 {
		t.Fatalf("predispatch refusal consumed original update: %s/%d", state, offset)
	}
	b.Close()
	reloaded, err := Open(filepath.Dir(b.path), h)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	f2 := &fakeClient{updates: make(chan []tg.Update, 1)}
	reloaded.newClient = func(string) (client, error) { return f2, nil }
	f2.updates <- []tg.Update{message(1, 10, 20, "original input")}
	reloaded.launch(f2)
	time.Sleep(150 * time.Millisecond)
	reloaded.mu.Lock()
	state, offset = reloaded.state.Inputs["telegram:123:1"], reloaded.state.Offset
	reloaded.mu.Unlock()
	if state != "deferred" || offset != 2 {
		t.Fatal("restart consumed input while recovery still active", state, offset)
	}
	e.mu.Lock()
	e.recovery.Automatic = false
	e.mu.Unlock()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reloaded.mu.Lock()
		state, offset = reloaded.state.Inputs["telegram:123:1"], reloaded.state.Offset
		reloaded.mu.Unlock()
		if state == "accepted" && offset == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	e.mu.Lock()
	submits := append([]api.Submission(nil), e.submits...)
	e.mu.Unlock()
	if state != "accepted" || offset != 2 || len(submits) != 1 || submits[0].ID != "telegram:123:1" {
		t.Fatalf("original update was not submitted once after restart: %s/%d/%v", state, offset, submits)
	}
}

func TestReadyRecoveryQueuesOriginalInputUntilFinalSuccessAndRestart(t *testing.T) {
	e := &ingressRecoveryEngine{snapshot: api.Snapshot{Connection: "ready", CanSend: true}, recovery: api.RecoveryState{Fence: "owner:9", Automatic: true}}
	s := backend.NewService(e, nil, nil, nil, nil)
	h := Host{Snapshot: s.Snapshot, Recovery: s.RecoveryState, Submit: func(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
		return backend.SubmitRemote(ctx, s, in, files)
	}}
	b, f := testBridge(t, h)
	paired(b)
	b.mu.Lock()
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatal(err)
	}
	b.mu.Unlock()
	f.updates <- []tg.Update{message(1, 10, 20, "queued during cleanup")}
	b.launch(f)
	time.Sleep(150 * time.Millisecond)
	e.mu.Lock()
	if len(e.submits) != 0 {
		e.mu.Unlock()
		t.Fatal("native input dispatched during recovery")
	}
	e.mu.Unlock()
	b.mu.Lock()
	if len(b.state.Inputs) != 0 || b.state.Offset != 2 || len(b.state.Ingress) != 1 {
		b.mu.Unlock()
		t.Fatal("original update was consumed during recovery")
	}
	b.mu.Unlock()
	e.mu.Lock()
	e.recovery.Automatic = false
	e.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		outcome, offset := b.state.Inputs["telegram:123:1"], b.state.Offset
		b.mu.Unlock()
		if outcome == "accepted" && offset == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	b.mu.Lock()
	outcome, offset := b.state.Inputs["telegram:123:1"], b.state.Offset
	b.mu.Unlock()
	if outcome != "accepted" || offset != 2 {
		t.Fatalf("original update was not accepted once after recovery: %s/%d", outcome, offset)
	}
	e.mu.Lock()
	if len(e.submits) != 1 || e.submits[0].ID != "telegram:123:1" {
		e.mu.Unlock()
		t.Fatal("original native receipt changed or replayed")
	}
	e.mu.Unlock()
	b.Close()
	reloaded, err := Open(filepath.Dir(b.path), h)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if !reloaded.input(t.Context(), f, message(1, 10, 20, "queued during cleanup")) {
		t.Fatal("reloaded bridge could not read original update")
	}
	e.mu.Lock()
	count := len(e.submits)
	e.mu.Unlock()
	if count != 1 || reloaded.state.Offset != 2 || reloaded.state.Inputs["telegram:123:1"] != "accepted" {
		t.Fatal("restart replayed or lost original receipt")
	}
}

func TestReadyRecoveryFinalFailureRejectsWithoutNativeDispatch(t *testing.T) {
	e := &ingressRecoveryEngine{snapshot: api.Snapshot{Connection: "ready", CanSend: true}, recovery: api.RecoveryState{Fence: "owner:9", Automatic: true}}
	s := backend.NewService(e, nil, nil, nil, nil)
	b, f := testBridge(t, Host{Snapshot: s.Snapshot, Recovery: s.RecoveryState, Submit: func(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
		return backend.SubmitRemote(ctx, s, in, files)
	}})
	paired(b)
	f.updates <- []tg.Update{message(1, 10, 20, "queued during cleanup")}
	b.launch(f)
	time.Sleep(150 * time.Millisecond)
	e.mu.Lock()
	e.snapshot.Connection, e.snapshot.CanSend = "offline", false
	e.recovery.Automatic, e.recovery.Manual = false, true
	e.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		outcome := b.state.Inputs["telegram:123:1"]
		offset := b.state.Offset
		b.mu.Unlock()
		if outcome == "rejected" && offset == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	b.mu.Lock()
	outcome, offset := b.state.Inputs["telegram:123:1"], b.state.Offset
	b.mu.Unlock()
	e.mu.Lock()
	count := len(e.submits)
	e.mu.Unlock()
	if outcome != "rejected" || offset != 2 || count != 0 {
		t.Fatalf("failed recovery delivered queued input: %s/%d/%d", outcome, offset, count)
	}
}

func TestTelegramRecoveryButtonChecksOriginalOwnerAndMessage(t *testing.T) {
	var mu sync.Mutex
	snapshot := api.Snapshot{Connection: "offline", ConnectionIssue: "resource_exhausted", Phase: "unknown"}
	state := api.RecoveryState{Fence: "original-owner:9", Manual: true}
	var calls atomic.Int32
	started := make(chan string, 1)
	var f *fakeClient
	h := Host{
		Snapshot: func() api.Snapshot { mu.Lock(); defer mu.Unlock(); return snapshot },
		Recovery: func() api.RecoveryState { mu.Lock(); defer mu.Unlock(); return state },
		Recover: func(_ context.Context, fence string) error {
			f.mu.Lock()
			answered := f.answers
			f.mu.Unlock()
			if answered == 0 {
				t.Error("recovery started before answering the Telegram callback")
			}
			calls.Add(1)
			started <- fence
			return nil
		},
		Chinese: func() bool { return true },
	}
	b, client := testBridge(t, h)
	f = client
	paired(b)
	b.mirrorRecovery(t.Context(), f, snapshot)
	f.mu.Lock()
	if len(f.keyboards) != 1 || f.keyboards[0] == nil || len(f.keyboards[0].InlineKeyboard) != 1 {
		f.mu.Unlock()
		t.Fatal("manual recovery did not produce one actionable button")
	}
	button := f.keyboards[0].InlineKeyboard[0][0]
	f.mu.Unlock()
	if button.Text != "重新连接" || len(button.CallbackData) > 64 || len(button.CallbackData) < 3 || b.state.Messages[recoveryMessageKey].Keyboard == "" {
		t.Fatal("recovery button lost its localized label or bounded callback", button)
	}
	valid := recoveryQuery("first", button.CallbackData, 1)
	foreignMessage := recoveryQuery("foreign", button.CallbackData, 999)
	b.callback(t.Context(), f, foreignMessage)
	foreignUser := recoveryQuery("foreign-user", button.CallbackData, 1)
	foreignUser.From.ID = 21
	b.callback(t.Context(), f, foreignUser)
	expired := recoveryQuery("expired", recoveryCallbackID(b.recoveryNonce, state.Fence, recoveryWindow()-1), 1)
	b.callback(t.Context(), f, expired)
	b.callback(t.Context(), f, valid)
	select {
	case got := <-started:
		if got != state.Fence {
			t.Fatal("recovery changed the original owner fence", got)
		}
	case <-time.After(time.Second):
		t.Fatal("authorized callback did not start recovery")
	}
	b.callback(t.Context(), f, valid)
	b.recoveryWait.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("recovery calls = %d, want one", got)
	}
	f.mu.Lock()
	answered := f.answers
	f.mu.Unlock()
	if answered != 4 {
		t.Fatalf("callback answers = %d, want original/foreign-message/expired/duplicate", answered)
	}
	mu.Lock()
	snapshot.Connection, snapshot.ConnectionIssue = "ready", ""
	state.Manual = false
	mu.Unlock()
	b.mirrorRecovery(t.Context(), f, snapshot)
	if b.state.Messages[recoveryMessageKey].Keyboard != "" || len(b.state.Inputs) != 0 {
		t.Fatal("recovery did not remove the old button or polluted input receipts")
	}
	b.callback(t.Context(), f, valid)
	if calls.Load() != 1 {
		t.Fatal("stale button replayed recovery after readiness")
	}
	mu.Lock()
	state = api.RecoveryState{Fence: "new-owner:10", Manual: true}
	snapshot.Connection = "offline"
	mu.Unlock()
	b.callback(t.Context(), f, valid)
	if calls.Load() != 1 {
		t.Fatal("old button recovered a newer Runtime owner")
	}
}

func TestTelegramPollsRecoveryControlWhileRuntimeOffline(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{}, 1)
	snapshot := api.Snapshot{Connection: "offline", Phase: "unknown"}
	b, f := testBridge(t, Host{
		Snapshot: func() api.Snapshot { return snapshot },
		Recovery: func() api.RecoveryState { return api.RecoveryState{Fence: "owner:offline", Manual: true} },
		Recover:  func(context.Context, string) error { calls.Add(1); started <- struct{}{}; return nil },
		Submit: func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
			t.Error("offline input reached native Runtime before the history boundary")
			return api.Receipt{}, nil
		},
	})
	paired(b)
	b.launch(f)
	waitFakeSends(t, f, 1)
	f.updates <- []tg.Update{message(1, 10, 20, "not sent while offline")}
	deadline := time.Now().Add(time.Second)
	for {
		b.mu.Lock()
		outcome := b.state.Inputs["telegram:123:1"]
		b.mu.Unlock()
		if outcome == "rejected" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("offline poll did not reject ordinary input")
		}
		time.Sleep(time.Millisecond)
	}
	f.mu.Lock()
	button := f.keyboards[0].InlineKeyboard[0][0].CallbackData
	f.mu.Unlock()
	f.updates <- []tg.Update{{UpdateID: 2, CallbackQuery: recoveryQuery("offline-callback", button, 1)}}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("offline poll did not route the recovery callback")
	}
	if calls.Load() != 1 {
		t.Fatal("offline control plane repeated recovery")
	}
}

func TestTelegramLeavesQueuedInputUntilAutomaticRecoveryEstablishesHistory(t *testing.T) {
	var mu sync.Mutex
	snapshot := api.Snapshot{Connection: "offline"}
	recovery := api.RecoveryState{Fence: "owner:retry", Automatic: true}
	var submissions atomic.Int32
	b, f := testBridge(t, Host{
		Snapshot: func() api.Snapshot { mu.Lock(); defer mu.Unlock(); return snapshot },
		Recovery: func() api.RecoveryState { mu.Lock(); defer mu.Unlock(); return recovery },
		Submit: func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
			submissions.Add(1)
			return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
		},
	})
	paired(b)
	f.updates <- []tg.Update{message(1, 10, 20, "queued during automatic recovery")}
	b.launch(f)
	time.Sleep(150 * time.Millisecond)
	b.mu.Lock()
	queued := len(b.state.Inputs) == 0
	b.mu.Unlock()
	if submissions.Load() != 0 || !queued {
		t.Fatal("automatic recovery consumed queued input before the history boundary")
	}
	mu.Lock()
	snapshot.Connection, recovery.Automatic = "ready", false
	mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for submissions.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if submissions.Load() != 1 {
		t.Fatal("queued message was not submitted once after recovery")
	}
}

func waitFakeSends(t *testing.T, f *fakeClient, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		f.mu.Lock()
		got := f.sends
		f.mu.Unlock()
		if got >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Telegram sends = %d, want %d", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}
