package telegram

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	tg "github.com/mymmrac/telego"
)

func recoveryQuery(id, data string, messageID int) *tg.CallbackQuery {
	return &tg.CallbackQuery{ID: id, Data: data, From: tg.User{ID: 20}, Message: &tg.Message{
		MessageID: messageID, From: &tg.User{ID: 123, IsBot: true}, Chat: tg.Chat{ID: 10, Type: "private"},
	}}
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
