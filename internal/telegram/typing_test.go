package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	tg "github.com/mymmrac/telego"
)

func TestTypingOnlyTracksConnectedMainTurn(t *testing.T) {
	base := api.Snapshot{Connection: "ready", CurrentTurn: "main", Phase: "working"}
	if !TypingForMainTurn(base) {
		t.Fatal("connected main work did not activate typing")
	}
	for _, phase := range []string{"completed", "interrupted", "failed", "unknown", "attention", "waiting_approval", "idle"} {
		v := base
		v.Phase = phase
		if TypingForMainTurn(v) {
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
		if TypingForMainTurn(v) {
			t.Fatalf("non-typing state activated chat action: %+v", v)
		}
	}
	for _, status := range []string{"pending", "sending", "sent", "unknown", "unavailable", ""} {
		v := base
		v.Approvals = []api.Approval{{Status: "resolved"}, {Status: status}}
		if TypingForMainTurn(v) {
			t.Fatalf("unresolved approval status %q activated typing", status)
		}
	}
	base.Approvals = []api.Approval{{Status: "resolved"}, {Status: "resolved"}}
	if !TypingForMainTurn(base) {
		t.Fatal("resolved history suppressed the active main turn")
	}
	base.Phase = "sending"
	if !TypingForMainTurn(base) {
		t.Fatal("confirmed sending did not activate typing")
	}
}

func TestTypingApprovalOwnerRequiresBackendTaskAuthority(t *testing.T) {
	base := api.Snapshot{Connection: "ready", CurrentTurn: "main", Phase: "working"}
	for _, status := range []string{"pending", "sent", "unknown"} {
		worker := base
		worker.Approvals = []api.Approval{{TurnKey: "worker-turn", Owner: "task", Status: status}}
		if !TypingForMainTurn(worker) {
			t.Fatalf("independent task %s paused main typing", status)
		}
		for _, owner := range []string{"conversation", "", "unrecognized"} {
			blocking := worker
			blocking.Approvals = []api.Approval{{TurnKey: "child-turn", Owner: owner, Status: status}}
			if TypingForMainTurn(blocking) {
				t.Fatalf("%q owner %s bypassed the main approval gate", owner, status)
			}
		}
	}
	base.Approvals = []api.Approval{{TurnKey: "worker-turn", Owner: "task", Status: "pending"}}
	base.CurrentTurn = ""
	if TypingForMainTurn(base) {
		t.Fatal("worker-only work emitted main-turn typing")
	}
}

func TestTypingBridgeKeepsMainWorkActiveBesideTaskApproval(t *testing.T) {
	var mu sync.Mutex
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "main", Phase: "working",
		Approvals: []api.Approval{{ID: "worker-native", TurnKey: "worker-turn", Owner: "task", Status: "pending"}}}
	b, f := testBridge(t, Host{Snapshot: func() api.Snapshot {
		mu.Lock()
		defer mu.Unlock()
		view := snapshot
		view.Approvals = append([]api.Approval(nil), snapshot.Approvals...)
		return view
	}})
	paired(b)
	b.typingPoll, b.typingRenew = 5*time.Millisecond, 25*time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); b.typing(ctx, f) }()
	defer func() { cancel(); <-done }()
	waitFakeActions(t, f, 2)
	for _, status := range []string{"sent", "unknown"} {
		mu.Lock()
		snapshot.Approvals[0].Status = status
		mu.Unlock()
		waitFakeActions(t, f, fakeActionCount(f)+2)
	}
	mu.Lock()
	snapshot.Approvals = append(snapshot.Approvals, api.Approval{ID: "main-native", TurnKey: "main", Owner: "conversation", Status: "pending"})
	mu.Unlock()
	assertTypingPaused(t, f)
	mu.Lock()
	snapshot.Approvals[1].Status = "unknown"
	mu.Unlock()
	assertTypingPaused(t, f)
	mu.Lock()
	snapshot.Approvals[1].Status = "resolved"
	mu.Unlock()
	waitFakeActions(t, f, fakeActionCount(f)+2)
	mu.Lock()
	snapshot.CurrentTurn = ""
	mu.Unlock()
	assertTypingPaused(t, f)
	for _, chat := range fakeActionChats(f) {
		if chat != 10 {
			t.Fatalf("typing escaped paired chat: %d", chat)
		}
	}
}

func TestTypingResultClassifiesSDKWrappedDeadline(t *testing.T) {
	if result, code := typingActionResult(&transportError{issue: "network"}, true); result != "timeout" || code != 0 {
		t.Fatalf("SDK-wrapped deadline classified as %s/%d", result, code)
	}
}

func TestTypingResumesAfterNativeApprovalAndFutureMainTurn(t *testing.T) {
	var mu sync.Mutex
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "main-1", Phase: "working"}
	var records []diagnosticlog.Record
	b, f := testBridge(t, Host{
		Snapshot: func() api.Snapshot {
			mu.Lock()
			defer mu.Unlock()
			view := snapshot
			view.Approvals = append([]api.Approval(nil), snapshot.Approvals...)
			return view
		},
		Diagnostics: func(r diagnosticlog.Record) { mu.Lock(); defer mu.Unlock(); records = append(records, r) },
	})
	paired(b)
	b.typingPoll, b.typingRenew = 5*time.Millisecond, 25*time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); b.typing(ctx, f) }()
	defer func() { cancel(); <-done }()
	waitFakeActions(t, f, 2)
	mu.Lock()
	snapshot.Approvals = []api.Approval{{ID: "private-approval-id", Status: "pending"}}
	mu.Unlock()
	assertTypingPaused(t, f)
	mu.Lock()
	snapshot.Approvals[0].Status = "sent"
	mu.Unlock()
	assertTypingPaused(t, f)
	mu.Lock()
	snapshot.Approvals[0].Status = "resolved"
	mu.Unlock()
	waitFakeActions(t, f, fakeActionCount(f)+2)
	mu.Lock()
	snapshot.Phase, snapshot.CurrentTurn = "completed", ""
	mu.Unlock()
	assertTypingPaused(t, f)
	mu.Lock()
	snapshot.Phase, snapshot.CurrentTurn = "working", "main-2" // Historical resolved card remains.
	mu.Unlock()
	waitFakeActions(t, f, fakeActionCount(f)+2)
	mu.Lock()
	snapshot.CurrentTurn = "" // Standalone Worker remains active.
	mu.Unlock()
	assertTypingPaused(t, f)

	mu.Lock()
	got := append([]diagnosticlog.Record(nil), records...)
	mu.Unlock()
	var sawPause, sawActive, sawOK bool
	for _, r := range got {
		if r.Method != "" || r.Thread != "" || r.Turn != "" || r.Item != "" || r.Reason != "" {
			t.Fatal("typing diagnostics included private event fields")
		}
		sawPause = sawPause || r.Code == "typing_state" && r.Phase == "approval_unresolved"
		sawActive = sawActive || r.Code == "typing_state" && r.Phase == "active"
		sawOK = sawOK || r.Code == "typing_action_result" && r.Phase == "ok"
	}
	if !sawPause || !sawActive || !sawOK || len(got) > 16 {
		t.Fatalf("missing or noisy state/result diagnostics: pause=%t active=%t ok=%t count=%d", sawPause, sawActive, sawOK, len(got))
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if contains := string(encoded); strings.Contains(contains, "private-approval-id") || strings.Contains(contains, "main-1") {
		t.Fatal("typing diagnostics leaked native identifiers")
	}
	for _, chat := range fakeActionChats(f) {
		if chat != 10 {
			t.Fatalf("action sent to unpaired chat %d", chat)
		}
	}
}

func assertTypingPaused(t *testing.T, f *fakeClient) {
	t.Helper()
	time.Sleep(15 * time.Millisecond)
	count := fakeActionCount(f)
	time.Sleep(75 * time.Millisecond)
	if got := fakeActionCount(f); got != count {
		t.Fatalf("typing renewed during pause: %d -> %d", count, got)
	}
}

func fakeActionChats(f *fakeClient) []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.chatActions...)
}

// A full production renewal interval proves that a long main turn receives a
// fresh action before Telegram's approximately five-second display expires.
func TestTypingLongMainTurnRenewsBeforeFiveSeconds(t *testing.T) {
	b, f := testBridge(t, Host{Snapshot: func() api.Snapshot {
		return api.Snapshot{Connection: "ready", CurrentTurn: "main", Phase: "working", Approvals: []api.Approval{{Status: "resolved"}}}
	}})
	paired(b)
	c := &delayedActionClient{fakeClient: f, started: make(chan time.Time, 3), delay: 1200 * time.Millisecond}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); b.typing(ctx, c) }()
	defer func() { cancel(); <-done }()
	var first, second time.Time
	select {
	case first = <-c.started:
	case <-time.After(time.Second):
		t.Fatal("first typing request did not start")
	}
	select {
	case second = <-c.started:
	case <-time.After(4800 * time.Millisecond):
		t.Fatal("slow first request pushed renewal past Telegram's typing lifetime")
	}
	if gap := second.Sub(first); gap > 4800*time.Millisecond {
		t.Fatalf("slow typing call stretched renewal gap to %s", gap)
	}
	time.Sleep(time.Until(first.Add(5500 * time.Millisecond)))
	if got := fakeActionCount(f); got < 2 {
		t.Fatalf("long working turn had only %d typing action", got)
	}
}

type delayedActionClient struct {
	*fakeClient
	started chan time.Time
	delay   time.Duration
}

func (c *delayedActionClient) ChatAction(ctx context.Context, chat int64) error {
	c.started <- time.Now()
	select {
	case <-time.After(c.delay):
		return c.fakeClient.ChatAction(ctx, chat)
	case <-ctx.Done():
		return ctx.Err()
	}
}

type blockedActionClient struct {
	*fakeClient
	started chan struct{}
}

func (c *blockedActionClient) ChatAction(ctx context.Context, chat int64) error {
	select {
	case c.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestSlowTypingCallDoesNotBlockMessageStopOrApproval(t *testing.T) {
	var mu sync.Mutex
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "main", Phase: "working"}
	stopped, decided := make(chan struct{}, 1), make(chan struct{}, 1)
	b, f := testBridge(t, Host{
		Snapshot:  func() api.Snapshot { mu.Lock(); defer mu.Unlock(); return snapshot },
		Interrupt: func(context.Context) error { stopped <- struct{}{}; return nil },
		Decide: func(_ context.Context, d api.Decision) error {
			if d.Choice != "accept" {
				t.Errorf("wrong choice: %s", d.Choice)
			}
			decided <- struct{}{}
			return nil
		},
	})
	paired(b)
	c := &blockedActionClient{fakeClient: f, started: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); b.typing(ctx, c) }()
	defer func() { cancel(); <-done; b.recoveryWait.Wait() }()
	select {
	case <-c.started:
	case <-time.After(time.Second):
		t.Fatal("typing call did not start")
	}
	start := time.Now()
	if !b.input(t.Context(), c, message(10, 10, 20, "/status")) || time.Since(start) > 500*time.Millisecond {
		t.Fatal("slow typing blocked ordinary status message")
	}
	start = time.Now()
	if !b.input(t.Context(), c, message(11, 10, 20, "/stop")) || time.Since(start) > 500*time.Millisecond {
		t.Fatal("slow typing blocked stop ingress")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop not dispatched")
	}
	approval := api.Approval{ID: "native-approval", Status: "pending", Title: "Fixture approval", Choices: []api.Choice{{ID: "accept", LabelKey: "chat.allowOnce", Scope: "once"}}}
	mu.Lock()
	snapshot.Approvals = []api.Approval{approval}
	current := snapshot
	mu.Unlock()
	start = time.Now()
	b.mirror(t.Context(), c, current)
	card := b.state.Messages["approval:"+approval.ID]
	q := &tg.CallbackQuery{ID: "fixture-callback", From: tg.User{ID: 20}, Message: &tg.Message{MessageID: card.IDs[0], Chat: tg.Chat{ID: 10}}, Data: callbackID(approval, "accept")}
	if !b.callback(t.Context(), c, q) || time.Since(start) > 500*time.Millisecond {
		t.Fatal("slow typing blocked approval callback")
	}
	select {
	case <-decided:
	case <-time.After(time.Second):
		t.Fatal("approval not dispatched")
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
	var mu sync.Mutex
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "main", Phase: "working"}
	stopped, decided := make(chan struct{}, 1), make(chan struct{}, 1)
	var records []diagnosticlog.Record
	b, f := testBridge(t, Host{
		Snapshot:  func() api.Snapshot { mu.Lock(); defer mu.Unlock(); return snapshot },
		Interrupt: func(context.Context) error { stopped <- struct{}{}; return nil },
		Decide: func(_ context.Context, d api.Decision) error {
			if d.Choice != "accept" {
				t.Errorf("wrong choice: %s", d.Choice)
			}
			decided <- struct{}{}
			return nil
		},
		Diagnostics: func(r diagnosticlog.Record) { mu.Lock(); defer mu.Unlock(); records = append(records, r) },
	})
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
	if !b.input(t.Context(), f, message(2, 10, 20, "/stop")) {
		t.Fatal("429 blocked stop ingress")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("429 blocked stop dispatch")
	}
	approval := api.Approval{ID: "rate-limited-approval", Status: "pending", Title: "Fixture approval", Choices: []api.Choice{{ID: "accept", LabelKey: "chat.allowOnce", Scope: "once"}}}
	mu.Lock()
	snapshot.Approvals = []api.Approval{approval}
	current := snapshot
	mu.Unlock()
	b.mirror(t.Context(), f, current)
	card := b.state.Messages["approval:"+approval.ID]
	q := &tg.CallbackQuery{ID: "rate-limited-callback", From: tg.User{ID: 20}, Message: &tg.Message{MessageID: card.IDs[0], Chat: tg.Chat{ID: 10}}, Data: callbackID(approval, "accept")}
	if !b.callback(t.Context(), f, q) {
		t.Fatal("429 blocked approval callback")
	}
	select {
	case <-decided:
	case <-time.After(time.Second):
		t.Fatal("429 blocked approval dispatch")
	}
	b.recoveryWait.Wait()
	mu.Lock()
	defer mu.Unlock()
	var saw429 bool
	for _, r := range records {
		if r.Code == "typing_action_result" && r.Phase == "rate_limited" && r.HTTPStatus == 429 {
			saw429 = true
		}
	}
	if !saw429 || len(records) > 6 {
		t.Fatalf("missing or noisy redacted 429 result: present=%t records=%d", saw429, len(records))
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
