package telegram

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestTypingOnlyTracksConnectedMainTurn(t *testing.T) {
	base := api.Snapshot{Connection: "ready", CurrentTurn: "main", Phase: "working"}
	if !typingForMainTurn(base) {
		t.Fatal("connected main work did not activate typing")
	}
	for _, phase := range []string{"completed", "interrupted", "failed", "unknown", "attention", "waiting_approval", "idle"} {
		v := base
		v.Phase = phase
		if typingForMainTurn(v) {
			t.Fatalf("terminal or attention phase %q kept typing", phase)
		}
	}
	for _, v := range []api.Snapshot{
		{Connection: "offline", CurrentTurn: "main", Phase: "working"},
		{Connection: "connecting", CurrentTurn: "main", Phase: "working"},
		{Connection: "ready", Phase: "working"}, // Independent Worker, no main turn.
		{Connection: "ready", CurrentTurn: "main", Phase: "working", Approvals: []api.Approval{{ID: "pending"}}},
		{Connection: "ready", CurrentTurn: "main", Phase: "working", LoginPending: true},
	} {
		if typingForMainTurn(v) {
			t.Fatalf("non-typing state activated chat action: %+v", v)
		}
	}
	base.Phase = "sending"
	if !typingForMainTurn(base) {
		t.Fatal("confirmed sending did not activate typing")
	}
}

func TestTypingRenewsOnceAndStopsOnCompletionPauseAndCancel(t *testing.T) {
	var mu sync.Mutex
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "main", Phase: "sending"}
	recovery := api.RecoveryState{Fence: "owner:9"}
	b, f := testBridge(t, Host{
		Snapshot: func() api.Snapshot { mu.Lock(); defer mu.Unlock(); return snapshot },
		Recovery: func() api.RecoveryState { mu.Lock(); defer mu.Unlock(); return recovery },
	})
	paired(b)
	b.typingPoll, b.typingRenew = 5*time.Millisecond, 25*time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); b.typing(ctx, f) }()
	defer func() { cancel(); <-done }()
	waitFakeActions(t, f, 3)
	mu.Lock()
	recovery.Automatic = true
	mu.Unlock()
	time.Sleep(15 * time.Millisecond)
	count := fakeActionCount(f)
	time.Sleep(70 * time.Millisecond)
	if got := fakeActionCount(f); got != count {
		t.Fatalf("ready snapshot kept typing during automatic recovery: %d -> %d", count, got)
	}
	mu.Lock()
	recovery.Automatic = false
	mu.Unlock()
	waitFakeActions(t, f, count+1)
	mu.Lock()
	snapshot.Phase = "completed"
	mu.Unlock()
	time.Sleep(15 * time.Millisecond)
	count = fakeActionCount(f)
	time.Sleep(70 * time.Millisecond)
	if got := fakeActionCount(f); got != count {
		t.Fatalf("typing renewed after completion: %d -> %d", count, got)
	}
	mu.Lock()
	snapshot.Phase = "working"
	mu.Unlock()
	waitFakeActions(t, f, count+1)
	b.mu.Lock()
	b.state.Enabled = false
	b.mu.Unlock()
	time.Sleep(15 * time.Millisecond)
	count = fakeActionCount(f)
	time.Sleep(70 * time.Millisecond)
	if got := fakeActionCount(f); got != count {
		t.Fatalf("typing renewed after pause: %d -> %d", count, got)
	}
	cancel()
	<-done
	count = fakeActionCount(f)
	time.Sleep(40 * time.Millisecond)
	if got := fakeActionCount(f); got != count || len(b.state.Messages) != 0 {
		t.Fatal("canceled typing kept sending or wrote a delivery receipt", got, b.state.Messages)
	}
}

func TestTypingRateLimitDoesNotBlockStatusOrRetryTooEarly(t *testing.T) {
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "main", Phase: "working"}
	b, f := testBridge(t, Host{Snapshot: func() api.Snapshot { return snapshot }})
	paired(b)
	b.typingPoll, b.typingRenew = 5*time.Millisecond, 25*time.Millisecond
	f.mu.Lock()
	f.chatActionErr = &transportError{issue: "rate_limited", code: 429, retry: 1}
	f.mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); b.typing(ctx, f) }()
	defer func() { cancel(); <-done }()
	waitFakeActions(t, f, 1)
	start := time.Now()
	if !b.input(t.Context(), f, message(1, 10, 20, "/status")) || time.Since(start) > 200*time.Millisecond {
		t.Fatal("typing rate limit blocked Telegram status processing")
	}
	b.mu.Lock()
	b.state.Enabled = false
	b.mu.Unlock()
	time.Sleep(15 * time.Millisecond)
	b.mu.Lock()
	b.state.Enabled = true
	b.mu.Unlock()
	time.Sleep(90 * time.Millisecond)
	if got := fakeActionCount(f); got != 1 {
		t.Fatalf("429 retry_after ignored; actions = %d", got)
	}
	f.mu.Lock()
	sends := f.sends
	f.mu.Unlock()
	if sends != 1 {
		t.Fatalf("rate-limited chat action blocked ordinary status reply: %d", sends)
	}
}

func TestTypingBridgeDisconnectAndForgetJoinLoop(t *testing.T) {
	b, f := testBridge(t, Host{Snapshot: func() api.Snapshot {
		return api.Snapshot{Connection: "ready", CurrentTurn: "main", Phase: "working"}
	}})
	paired(b)
	b.typingPoll, b.typingRenew = 5*time.Millisecond, 20*time.Millisecond
	b.launch(f)
	waitFakeActions(t, f, 2)
	if _, err := b.Disconnect(); err != nil {
		t.Fatal(err)
	}
	count := fakeActionCount(f)
	time.Sleep(50 * time.Millisecond)
	if got := fakeActionCount(f); got != count {
		t.Fatalf("typing continued after disconnect: %d -> %d", count, got)
	}
	if _, err := b.Forget(); err != nil {
		t.Fatal(err)
	}
	if got := fakeActionCount(f); got != count {
		t.Fatal("unpair restarted typing", got)
	}
}

func fakeActionCount(f *fakeClient) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.chatActions)
}

func waitFakeActions(t *testing.T, f *fakeClient, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		if got := fakeActionCount(f); got >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("typing actions = %d, want %d", fakeActionCount(f), want)
		}
		time.Sleep(time.Millisecond)
	}
}
