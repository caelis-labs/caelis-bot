package weixin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

func sentTexts(t *testing.T, handler func(string) string) (*httptest.Server, *[]string) {
	t.Helper()
	texts := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Msg struct {
				Items []messageItem `json:"item_list"`
			} `json:"msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Msg.Items) != 1 || request.Msg.Items[0].Text == nil {
			t.Errorf("send request: %v", err)
			return
		}
		text := request.Msg.Items[0].Text.Text
		texts = append(texts, text)
		response := `{"ret":0}`
		if handler != nil {
			response = handler(text)
		}
		_, _ = w.Write([]byte(response))
	}))
	return server, &texts
}

func TestTwelveCompletedCommentariesWaitForTurnFinal(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "turn", Phase: "working"}
	for i := 0; i < 12; i++ {
		snapshot.Items = append(snapshot.Items, api.Item{ID: "comment-" + string(rune('a'+i)), TurnKey: "turn", Kind: "assistant", Text: "progress", Status: "completed"})
	}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID, b.state.BotID = "owner", "bot"
	freshWindow(b, "input")
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	b.output(t.Context(), p)
	if len(*texts) != 0 || b.state.Window.Used != 0 {
		t.Fatal("completed commentary consumed the phone window")
	}
	snapshot.Phase = "completed"
	snapshot.Items = append(snapshot.Items, api.Item{ID: "final", TurnKey: "turn", Kind: "assistant", Text: "最终答复", Status: "completed"})
	b.output(t.Context(), p)
	if len(*texts) != 1 || (*texts)[0] != "最终答复" || b.state.Window.Used != 1 {
		t.Fatalf("final delivery: %#v, used=%d", *texts, b.state.Window.Used)
	}
}

func TestTerminalTurnObservationSurvivesNextTurn(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "old", Phase: "working", Items: []api.Item{{ID: "old-final", TurnKey: "old", Kind: "assistant", Text: "旧轮最终结果", Status: "completed"}}}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID, b.state.BotID = "owner", "bot"
	freshWindow(b, "input")
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	snapshot.CurrentTurn, snapshot.Phase = "new", "working"
	b.output(t.Context(), p)
	if len(*texts) != 0 {
		t.Fatalf("older unconfirmed Turn leaked commentary: %#v", *texts)
	}
	b.ObserveTurn(api.Snapshot{CurrentTurn: "old", Phase: "completed"})
	b.output(t.Context(), p)
	if len(*texts) != 1 || (*texts)[0] != "旧轮最终结果" {
		t.Fatalf("observed completion lost when newer Turn started: %#v", *texts)
	}
	snapshot.Items = append(snapshot.Items, api.Item{ID: "failed-item", TurnKey: "failed", Kind: "assistant", Text: "unfinished", Status: "completed"})
	b.ObserveTurn(api.Snapshot{CurrentTurn: "failed", Phase: "failed"})
	b.output(t.Context(), p)
	if len(*texts) != 1 {
		t.Fatalf("failed Turn sent a claimed final: %#v", *texts)
	}
}

func TestUnseededPairingDoesNotRememberHistoricalTurn(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "old", Phase: "completed", Items: []api.Item{{ID: "history", TurnKey: "old", Kind: "assistant", Text: "old answer", Status: "completed"}}}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID, b.state.BotID = "owner", "bot"
	b.ObserveTurn(snapshot)
	if len(b.state.TurnPhases) != 0 {
		t.Fatal("historical Turn was primed before first accepted input")
	}
	freshWindow(b, "new-input")
	snapshot.CurrentTurn, snapshot.Phase = "new", "working"
	b.output(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "fixture"})
	if len(*texts) != 0 {
		t.Fatalf("historical reply replayed after first ingress: %#v", *texts)
	}
}

func TestApprovalAndQuestionPrecedeLowValueMirrors(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	root := t.TempDir()
	control, err := textchannel.Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	question := api.Item{ID: "question", TurnKey: "turn", Kind: "assistant", Status: "completed", AsyncCallID: "native-call", AsyncQuestions: []api.AsyncQuestion{{Title: "选哪个？", Options: []string{"甲", "乙"}}}}
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "turn", RuntimeOwner: "runtime", Phase: "working", Approvals: []api.Approval{{ID: "approval", Owner: "conversation", Status: "pending", Choices: []api.Choice{{ID: "once", Label: "仅本次"}}}}, Items: []api.Item{question}}
	for i := 0; i < 12; i++ {
		snapshot.Items = append(snapshot.Items, api.Item{ID: "user-" + string(rune('a'+i)), Kind: "user", Text: "cross-entry", Status: "completed"})
	}
	b, err := Open(root, Host{Snapshot: func() api.Snapshot { return snapshot }, TextControl: control})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID, b.state.BotID = "owner", "bot"
	freshWindow(b, "input")
	b.output(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "fixture"})
	if len(*texts) != windowSoftLimit || !strings.Contains((*texts)[0], "/approve") || !strings.Contains((*texts)[1], "[Q1]") || b.state.Window.Used != windowSoftLimit || b.state.Missed != 6 {
		t.Fatalf("control priority or soft reserve: sent=%#v used=%d missed=%d", *texts, b.state.Window.Used, b.state.Missed)
	}
}

func TestLongFinalCompactsWhenOnlyOneSlotRemains(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	full := strings.Repeat("中文😀一段内容。\n", 400)
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "turn", Phase: "completed", Items: []api.Item{{ID: "final", TurnKey: "turn", Kind: "assistant", Status: "completed", Text: full}}}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID, b.state.BotID = "owner", "bot"
	freshWindow(b, "input")
	b.state.Window.Used = windowSendLimit - 1
	b.output(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "fixture"})
	if len(*texts) != 1 || !fitsText((*texts)[0]) || !strings.Contains((*texts)[0], "中间省略") || !strings.Contains((*texts)[0], "继续发送") || !strings.Contains((*texts)[0], "中文😀") || b.state.Window.Used != windowSendLimit {
		t.Fatalf("bounded final: count=%d used=%d", len(*texts), b.state.Window.Used)
	}
	if b.state.Outputs["item:final:compact"].State != "accepted" || b.state.Outputs["item:final:0"].State != "" {
		t.Fatal("compact result was duplicated by a full part")
	}
}

func TestWindowPersistsAndLegacyUnknownBudgetWaitsForNewInput(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	root := t.TempDir()
	b, err := Open(root, Host{})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID, b.state.BotID, b.state.ContextToken, b.state.BaseURL = "owner", "bot", "old-token", defaultBase
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	b.sendTextOnce(t.Context(), p, "legacy", "old-token", "cannot assume full budget")
	if len(*texts) != 0 {
		t.Fatal("legacy record invented a fresh send window")
	}
	freshWindow(b, "new-input")
	b.sendTextOnce(t.Context(), p, "one", "ctx", "first")
	reopened, err := Open(root, Host{})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.state.Window.Used != 1 || reopened.state.Window.InputID != "new-input" {
		t.Fatalf("window did not survive restart: %#v", reopened.state.Window)
	}
	reopened.sendTextOnce(t.Context(), p, "two", "ctx", "second")
	if len(*texts) != 2 || reopened.state.Window.Used != 2 {
		t.Fatal("restart granted a new budget or blocked remaining slots")
	}
}

func TestNewIngressDuringLateSendDoesNotConsumeFreshWindow(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ilink/bot/sendmessage" {
			once.Do(func() { close(entered) })
			<-release
			_, _ = w.Write([]byte(`{"ret":-2,"errmsg":"rate limited"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"next","msgs":[{"message_id":"new","from_user_id":"owner","to_user_id":"bot","message_type":1,"context_token":"new-context","item_list":[{"type":1,"text_item":{"text":"new input"}}]}]}`))
	}))
	defer server.Close()
	b, err := Open(t.TempDir(), Host{})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID, b.state.BotID = "owner", "bot"
	freshWindow(b, "old-input")
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	done := make(chan struct{})
	go func() { defer close(done); b.sendTextOnce(t.Context(), p, "old-result", "ctx", "late") }()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	received := make(chan struct{})
	go func() { defer close(received); b.receive(ctx, p) }()
	deadline := time.After(2 * time.Second)
	for {
		b.mu.Lock()
		newWindow := b.state.Window.InputID == "weixin:bot:new"
		b.mu.Unlock()
		if newWindow {
			break
		}
		select {
		case <-deadline:
			t.Fatal("new input was not stored during old send")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-received
	close(release)
	<-done
	if b.state.Window.InputID != "weixin:bot:new" || b.state.Window.Used != 0 || b.state.Window.Exhausted {
		t.Fatalf("late old rejection damaged fresh budget: %#v", b.state.Window)
	}
}

func TestDuplicateIngressCannotRefillConsumedWindow(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"next","msgs":[{"message_id":"same","from_user_id":"owner","to_user_id":"bot","message_type":1,"context_token":"forged-refill","item_list":[{"type":1,"text_item":{"text":"duplicate"}}]}]}`))
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
			_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"next","msgs":[]}`))
		}
	}))
	defer server.Close()
	b, err := Open(t.TempDir(), Host{})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID, b.state.BotID = "owner", "bot"
	freshWindow(b, "weixin:bot:same")
	b.state.Window.Used = 7
	b.state.Inputs["weixin:bot:same"] = "accepted"
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.receive(ctx, &protocol{client: server.Client(), base: server.URL, token: "fixture"})
	}()
	deadline := time.After(2 * time.Second)
	for {
		b.mu.Lock()
		advanced := b.state.Cursor == "next"
		b.mu.Unlock()
		if advanced {
			break
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("duplicate poll did not advance cursor")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done
	if b.state.Window.Used != 7 || b.state.Window.ContextToken != "ctx" || b.state.Window.InputID != "weixin:bot:same" {
		t.Fatalf("duplicate changed original window: %#v", b.state.Window)
	}
}

func TestPrepareFailureAndUnclassifiedRetDoNotImpersonateQuota(t *testing.T) {
	responses := []string{`{"ret":-2,"errmsg":"prepare failed"}`, `{"ret":-2}`}
	server, texts := sentTexts(t, func(string) string {
		response := responses[0]
		responses = responses[1:]
		return response
	})
	defer server.Close()
	b, err := Open(t.TempDir(), Host{})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID, b.state.BotID = "owner", "bot"
	freshWindow(b, "input")
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	b.sendTextOnce(t.Context(), p, "first", "ctx", "first")
	b.sendTextOnce(t.Context(), p, "second", "ctx", "second")
	if len(*texts) != 2 || b.state.Window.Exhausted || b.state.Window.Used != 2 || b.state.Outputs["first"].ErrorClass != "prepare_failed" || b.state.Outputs["second"].ErrorClass != "" {
		t.Fatalf("distinct failures collapsed into quota: first=%#v second=%#v window=%#v", b.state.Outputs["first"], b.state.Outputs["second"], b.state.Window)
	}
}

func TestUnclassifiedResponseDoesNotClaimAcceptanceOrReplay(t *testing.T) {
	server, texts := sentTexts(t, func(string) string { return `{"errmsg":"unexpected service state"}` })
	defer server.Close()
	b, err := Open(t.TempDir(), Host{})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID, b.state.BotID = "owner", "bot"
	freshWindow(b, "input")
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	b.sendTextOnce(t.Context(), p, "unclear", "ctx", "payload")
	b.sendTextOnce(t.Context(), p, "unclear", "ctx", "payload")
	if len(*texts) != 1 || b.state.Outputs["unclear"].State != "unknown" || b.state.Outputs["unclear"].ErrorClass != "other" {
		t.Fatalf("unclassified response was accepted or replayed: sent=%#v intent=%#v", *texts, b.state.Outputs["unclear"])
	}
}
