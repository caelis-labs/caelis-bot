package weixin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
