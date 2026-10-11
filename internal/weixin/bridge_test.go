package weixin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

func freshWindow(b *Bridge, id string) {
	b.state.ContextToken = "ctx"
	b.state.Window = sendWindow{OwnerID: b.state.OwnerID, InputID: id, ContextToken: "ctx", InputAt: time.Now().UnixMilli()}
}

func TestTextApprovalWorksWhileResidentSubmitIsGated(t *testing.T) {
	var sent atomic.Int32
	var texts []string
	var input []textchannel.Inbound
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Msg struct {
				Items []struct {
					Text struct {
						Text string `json:"text"`
					} `json:"text_item"`
				} `json:"item_list"`
			} `json:"msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Msg.Items) > 0 {
			texts = append(texts, request.Msg.Items[0].Text.Text)
		}
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()
	root := t.TempDir()
	control, err := textchannel.Open(root, func(_ context.Context, d api.Decision) error {
		if d.ID != "worker-approval" || d.Choice != "allow-once" {
			t.Errorf("wrong decision: %#v", d)
		}
		sent.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a := api.Approval{ID: "worker-approval", Owner: "task", TurnKey: "worker-turn", Status: "pending", Choices: []api.Choice{{ID: "allow-once", Label: "允许", Scope: "once"}, {ID: "deny", Label: "拒绝"}}}
	snapshot := api.Snapshot{Connection: "ready", Approvals: []api.Approval{a}}
	b, err := Open(root, Host{Snapshot: func() api.Snapshot { return snapshot }, Recovery: func() api.RecoveryState { return api.RecoveryState{InProgress: true} }, TextControl: control, RecordInput: func(in textchannel.Inbound, secret bool) {
		if secret {
			t.Error("ordinary command marked secret")
		}
		input = append(input, in)
	}, Submit: func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
		t.Fatal("control used ordinary Submit")
		return api.Receipt{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID, b.state.Enabled = "bot", "owner", true
	freshWindow(b, "prior-owner-input")
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	b.output(t.Context(), p)
	if len(texts) != 1 || !strings.Contains(texts[0], "[1] 允许（范围：仅本次）") || !strings.Contains(texts[0], "执行审批请输入\n/approve A1 1") {
		t.Fatalf("missing text card: %#v", texts)
	}
	b.state.Inbox = []inbound{{ID: "weixin:bot:1", Text: "/approve A1 1", ContextToken: "ctx"}}
	b.dispatch(t.Context(), p)
	b.dispatch(t.Context(), p)
	if len(input) != 1 || input[0].ID != "weixin:bot:1" || input[0].Conversation != "owner\x00ctx" || input[0].Text != "/approve A1 1" {
		t.Fatal("owner command was not journaled once", input)
	}
	if sent.Load() != 1 || len(texts) != 2 || !strings.Contains(texts[1], "决定已提交") {
		t.Fatalf("decision/receipt: %d %#v", sent.Load(), texts)
	}
	snapshot.Approvals[0].Status = "resolved"
	b.output(t.Context(), p)
	if len(texts) != 3 || !strings.Contains(texts[2], "已处理") {
		t.Fatalf("terminal mirror: %#v", texts)
	}
}

func TestLocalIMMigrationSeedsOnlyHistoricalOutputs(t *testing.T) {
	var snapshot api.Snapshot
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.LocalIMSeeded = false // Existing state written before this migration.
	snapshot = api.Snapshot{Connection: "offline", Items: []api.Item{
		{ID: "old", Kind: "assistant", Text: "old result"},
		{ID: "new", Kind: "assistant", Text: "new result", SeenAt: 1},
	}}
	b.output(t.Context(), nil)
	if !b.state.LocalIMSeeded || b.state.Outputs["item:old"].State != "skip" {
		t.Fatal("old local IM output could replay", b.state.Outputs)
	}
	if _, exists := b.state.Outputs["item:new"]; exists {
		t.Fatal("new output suppressed by migration")
	}
}

func TestWeixinReplyUsesOnlyServerConfirmedCardMessageID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ret":0,"message_id":"998"}`))
	}))
	defer server.Close()
	root := t.TempDir()
	var decisions []api.Decision
	control, _ := textchannel.Open(root, func(_ context.Context, d api.Decision) error { decisions = append(decisions, d); return nil })
	a := api.Approval{ID: "native", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "deny", Label: "拒绝"}, {ID: "once", Label: "仅本次"}}}
	snap := api.Snapshot{Connection: "ready", Approvals: []api.Approval{a}}
	submits := 0
	b, _ := Open(root, Host{Snapshot: func() api.Snapshot { return snap }, Recovery: func() api.RecoveryState { return api.RecoveryState{InProgress: true} }, TextControl: control, Submit: func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
		submits++
		return api.Receipt{}, nil
	}})
	b.state.BotID, b.state.OwnerID, b.state.ContextToken = "bot", "owner", "ctx"
	freshWindow(b, "prior-owner-input")
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	b.mirrorControls(t.Context(), p, snap)
	if !control.HasCardReference(textchannel.Inbound{Channel: "weixin", Conversation: "ctx", Reference: &textchannel.Reference{Conversation: "owner", MessageID: "998"}}) {
		t.Fatal("server card ID not bound")
	}
	b.state.Inbox = []inbound{{ID: "weixin:bot:1", Text: "2", ContextToken: "ctx", ReferenceID: "998"}}
	b.dispatch(t.Context(), p)
	if submits != 0 || len(decisions) != 1 || decisions[0].Choice != "once" {
		t.Fatalf("reply: submits=%d %#v", submits, decisions)
	}
	b.state.Inbox = []inbound{{ID: "weixin:bot:2", Text: "1", ContextToken: "ctx", ReferenceID: "998"}}
	b.dispatch(t.Context(), p)
	if len(decisions) != 1 {
		t.Fatal("stale reference replayed")
	}
}

func TestWeixinRefMessageIDAndSendResponseAreLossless(t *testing.T) {
	var msg message
	if err := json.Unmarshal([]byte(`{"message_id":18446744073709551615,"item_list":[{"type":1,"text_item":{"text":"2"},"ref_msg":{"svr_id":18446744073709551614,"message_item":{"type":1,"text_item":{"text":"forged A1"}}}}]}`), &msg); err != nil {
		t.Fatal(err)
	}
	if referenceOf(msg) != "18446744073709551614" {
		t.Fatalf("reference: %#v", msg)
	}
	b, _ := Open(t.TempDir(), Host{})
	if quoted := b.quotedMessageLocked(msg); quoted == nil || quoted.HostID != "18446744073709551614" || quoted.Text != "forged A1" {
		t.Fatalf("quoted metadata: %#v", quoted)
	}
	var result sendResult
	if err := json.Unmarshal([]byte(`{"ret":0,"message_id":18446744073709551614}`), &result); err != nil || string(result.MessageID) != referenceOf(msg) {
		t.Fatalf("response: %#v %v", result, err)
	}
}

func TestWeixinMissingServerMessageIDDoesNotBindCard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"ret":0}`)) }))
	defer server.Close()
	root := t.TempDir()
	control, _ := textchannel.Open(root, nil)
	a := api.Approval{ID: "native", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "deny", Label: "拒绝"}}}
	b, _ := Open(root, Host{TextControl: control})
	b.state.OwnerID, b.state.ContextToken = "owner", "ctx"
	freshWindow(b, "prior-owner-input")
	b.mirrorControls(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "fixture"}, api.Snapshot{Approvals: []api.Approval{a}})
	if control.HasCardReference(textchannel.Inbound{Channel: "weixin", Conversation: "ctx", Reference: &textchannel.Reference{Conversation: "owner", MessageID: "999"}}) {
		t.Fatal("invented Weixin delivery ID")
	}
}

func TestWeixinOrdinaryQuotedReplyWithoutServerIDStillReachesModelContext(t *testing.T) {
	var msg message
	if err := json.Unmarshal([]byte(`{"message_id":"82","from_user_id":"owner","to_user_id":"bot","message_type":1,"context_token":"ctx","item_list":[{"type":1,"text_item":{"text":"本次问题"},"ref_msg":{"message_item":{"type":1,"text_item":{"text":"旧消息选段"}},"partial_text":{"start":"旧","end":"段","startindex":0,"endindex":5}}}]}`), &msg); err != nil {
		t.Fatal(err)
	}
	var submitted api.Submission
	b, _ := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return api.Snapshot{Connection: "ready"} }, Recovery: func() api.RecoveryState { return api.RecoveryState{} }, Submit: func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		submitted = in
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	}})
	b.state.BotID, b.state.OwnerID = "bot", "owner"
	if referenceOf(msg) != "" {
		t.Fatal("missing server ID became approval authority")
	}
	quoted := b.quotedMessageLocked(msg)
	b.state.Inbox = []inbound{{ID: "weixin:bot:82", Text: textOf(msg), ContextToken: msg.ContextToken, Quoted: quoted}}
	b.dispatch(t.Context())
	if submitted.Text != "本次问题" || submitted.Quoted == nil || submitted.Quoted.HostID != "" || submitted.Quoted.Text != "旧消息选段" || !submitted.Quoted.Excerpt || submitted.Quoted.Role != "unknown" {
		t.Fatalf("quoted transport: %#v", submitted)
	}
	if !strings.Contains(submitted.ModelInputText(), "旧消息选段") || !strings.HasSuffix(submitted.ModelInputText(), "本次问题") {
		t.Fatal(submitted.ModelInputText())
	}
}

func TestWeixinMarkedSecretQuoteIsNotModelContext(t *testing.T) {
	var msg message
	if err := json.Unmarshal([]byte(`{"message_id":"82","item_list":[{"type":1,"text_item":{"text":"说明用途"},"ref_msg":{"svr_id":"70","message_item":{"type":1,"text_item":{"text":"PRIVATE_QUOTE_SENTINEL"}}}}]}`), &msg); err != nil {
		t.Fatal(err)
	}
	b, _ := Open(t.TempDir(), Host{})
	b.state.SecretMessages["70"] = true
	if b.quotedMessageLocked(msg) != nil {
		t.Fatal("marked secret quote entered model context")
	}
}

func TestMultiQuestionWeixinTextCompletesOneNativeRequest(t *testing.T) {
	var texts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Msg struct {
				Items []struct {
					Text struct {
						Text string `json:"text"`
					} `json:"text_item"`
				} `json:"item_list"`
			} `json:"msg"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if len(request.Msg.Items) > 0 {
			texts = append(texts, request.Msg.Items[0].Text.Text)
		}
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()
	root := t.TempDir()
	var decisions []api.Decision
	control, _ := textchannel.Open(root, func(_ context.Context, d api.Decision) error { decisions = append(decisions, d); return nil })
	a := api.Approval{ID: "original-worker", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "answer", Label: "提交回答"}}, Questions: []api.Question{{ID: "topic", Title: "主题", Type: "text", Required: true}, {ID: "depth", Title: "深度", Type: "select", Required: true, Options: []api.Choice{{ID: "brief", Label: "简要"}, {ID: "full", Label: "完整"}}}}}
	snapshot := api.Snapshot{Connection: "ready", Approvals: []api.Approval{a}}
	submits := 0
	b, _ := Open(root, Host{Snapshot: func() api.Snapshot { return snapshot }, Recovery: func() api.RecoveryState { return api.RecoveryState{InProgress: true} }, Submit: func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
		submits++
		return api.Receipt{}, nil
	}, TextControl: control})
	b.state.BotID, b.state.OwnerID, b.state.Enabled = "bot", "owner", true
	freshWindow(b, "prior-owner-input")
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	b.output(t.Context(), p)
	if len(texts) != 1 || !strings.Contains(texts[0], "[Q1] 主题") || !strings.Contains(texts[0], "[Q2] 深度") || !strings.Contains(texts[0], "/answer Q2 1") {
		t.Fatalf("missing two phone fields: %#v", texts)
	}
	b.state.Inbox = []inbound{{ID: "weixin:bot:1", Text: "/answer Q1 draft", ContextToken: "ctx"}}
	b.dispatch(t.Context(), p)
	if len(decisions) != 0 || submits != 0 || !strings.Contains(strings.Join(texts, "\n"), "仍需回答 Q2") {
		t.Fatalf("first field submitted early: %#v, submit=%d, texts=%#v", decisions, submits, texts)
	}
	b.state.Inbox = []inbound{{ID: "weixin:bot:2", Text: "/answer Q2 2", ContextToken: "ctx"}}
	b.dispatch(t.Context(), p)
	if len(decisions) != 1 || decisions[0].ID != "original-worker" || decisions[0].Answers["topic"][0] != "draft" || decisions[0].Answers["depth"][0] != "full" || submits != 0 {
		t.Fatalf("native completion: %#v, submit=%d", decisions, submits)
	}
}

func TestUserMirrorSkipsOnlyOwnIngress(t *testing.T) {
	var texts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Msg struct {
				Items []struct {
					Text struct {
						Text string `json:"text"`
					} `json:"text_item"`
				} `json:"item_list"`
			} `json:"msg"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if len(request.Msg.Items) > 0 {
			texts = append(texts, request.Msg.Items[0].Text.Text)
		}
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()
	root := t.TempDir()
	control, _ := textchannel.Open(root, nil)
	if err := control.RecordOrigin("remote-tg", textchannel.Origin{Channel: "telegram", Conversation: "chat"}); err != nil {
		t.Fatal(err)
	}
	if err := control.RecordOrigin("remote-wx", textchannel.Origin{Channel: "weixin", Conversation: "owner\x00ctx"}); err != nil {
		t.Fatal(err)
	}
	snapshot := api.Snapshot{Connection: "ready", Items: []api.Item{{ID: "u1", Kind: "user", RequestID: "remote-tg", Text: "hello", Status: "completed", TurnKey: "t1"}, {ID: "u2", Kind: "user", RequestID: "remote-wx", Text: "self", Status: "completed", TurnKey: "t2"}, {ID: "u3", Kind: "user", RequestID: "desktop", Text: "desktop steer", Status: "completed", TurnKey: "t2"}, {ID: "a1", Kind: "assistant", Text: "reply", Status: "completed", TurnKey: "t1"}}}
	b, err := Open(root, Host{Snapshot: func() api.Snapshot { return snapshot }, TextControl: control})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID, b.state.Enabled = "bot", "owner", true
	freshWindow(b, "prior-owner-input")
	b.state.Inputs["remote-wx"] = "accepted"
	b.ObserveTurn(api.Snapshot{CurrentTurn: "t1", Phase: "completed"})
	b.output(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "fixture"})
	if len(texts) != 3 || texts[0] != "User: hello" || texts[1] != "User: desktop steer" || texts[2] != "reply" {
		t.Fatalf("mirror: %#v", texts)
	}
}

func TestReceiveDurableOwnerOnlyAndNoDuplicate(t *testing.T) {
	var calls atomic.Int32
	releasePoll := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 1 {
			select {
			case <-r.Context().Done():
			case <-releasePoll:
			}
			return
		}
		_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"next-cursor","msgs":[{"message_id":18446744073709551615,"from_user_id":"owner","to_user_id":"bot","message_type":1,"context_token":"ctx","item_list":[{"type":1,"text_item":{"text":"hello"}}]},{"message_id":"18446744073709551615","from_user_id":"owner","to_user_id":"bot","message_type":1,"item_list":[{"type":1,"text_item":{"text":"duplicate"}}]},{"message_id":"2","from_user_id":"stranger","to_user_id":"bot","message_type":1,"item_list":[{"type":1,"text_item":{"text":"intruder"}}]},{"message_id":"3","from_user_id":"owner","to_user_id":"bot","group_id":"group","message_type":1,"item_list":[{"type":1,"text_item":{"text":"group"}}]}]}`))
	}))
	t.Cleanup(func() {
		close(releasePoll)
		server.Close()
	})
	b, err := Open(t.TempDir(), Host{})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID = "bot"
	b.state.OwnerID = "owner"
	b.state.BaseURL = defaultBase
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		b.receive(ctx, &protocol{client: server.Client(), base: server.URL, token: "secret"})
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for {
		b.mu.Lock()
		ready := b.state.Cursor == "next-cursor" && len(b.state.Inbox) == 1
		b.mu.Unlock()
		if ready {
			break
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("cursor and inbox not saved")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done
	b.mu.Lock()
	in := b.state.Inbox[0]
	window := b.state.Window
	b.mu.Unlock()
	if in.ID != "weixin:bot:18446744073709551615" || in.Text != "hello" || in.ContextToken != "ctx" {
		t.Fatalf("wrong inbox: %#v", in)
	}
	if window.InputID != in.ID || window.OwnerID != "owner" || window.ContextToken != "ctx" || window.Used != 0 {
		t.Fatalf("duplicate or foreign ingress refreshed the wrong budget: %#v", window)
	}
	info, err := os.Stat(b.path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private state permissions: %v %v", info, err)
	}
	reloaded, err := Open(strings.TrimSuffix(b.path, "/weixin.json"), Host{})
	if err != nil || reloaded.state.Cursor != "next-cursor" || len(reloaded.state.Inbox) != 1 {
		t.Fatalf("reopen: %#v %v", reloaded.state, err)
	}
}

func TestMarkedSecretControlIngressKeepsValueOutOfWeixinLedger(t *testing.T) {
	const secret = "SENTINEL_PRIVATE_ANSWER"
	var calls atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 1 {
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"cursor","msgs":[{"message_id":"1","from_user_id":"owner","to_user_id":"bot","message_type":1,"context_token":"ctx","item_list":[{"type":1,"text_item":{"text":"/answer Q1 SENTINEL_PRIVATE_ANSWER"}}]}]}`))
	}))
	t.Cleanup(func() { close(release); server.Close() })
	root := t.TempDir()
	var decisions []api.Decision
	control, _ := textchannel.Open(root, func(_ context.Context, d api.Decision) error { decisions = append(decisions, d); return nil })
	a := api.Approval{ID: "native", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "answer", Label: "提交"}}, Questions: []api.Question{{ID: "credential", Type: "text", Required: true, Secret: true}}}
	if _, id, err := control.Card(a); err != nil || id != "Q1" {
		t.Fatalf("card id %q: %v", id, err)
	}
	b, err := Open(root, Host{TextControl: control, Snapshot: func() api.Snapshot { return api.Snapshot{Connection: "ready", Approvals: []api.Approval{a}} }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID = "bot", "owner"
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		b.receive(ctx, &protocol{client: server.Client(), base: server.URL, token: "fixture"})
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for {
		b.mu.Lock()
		ready := len(b.state.Inbox) == 1
		b.mu.Unlock()
		if ready {
			break
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("secret ingress was not stored")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done
	b.mu.Lock()
	in := b.state.Inbox[0]
	b.mu.Unlock()
	encoded, err := os.ReadFile(b.path)
	if err != nil || !in.Secret || in.Text != "" || strings.Contains(string(encoded), secret) {
		t.Fatalf("secret entered ordinary inbox: %#v %v", in, err)
	}
	b.dispatch(t.Context())
	if len(decisions) != 1 || decisions[0].Answers["credential"][0] != secret {
		t.Fatalf("native secret answer: %#v", decisions)
	}
	encoded, err = os.ReadFile(b.path)
	if err != nil || strings.Contains(string(encoded), secret) {
		t.Fatal("secret entered durable delivery ledger", err)
	}
}

func TestUnknownSubmitAndSendAreNotReplayed(t *testing.T) {
	var submitted, sent atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer server.Close()
	root := t.TempDir()
	snapshot := api.Snapshot{Connection: "ready"}
	host := Host{Snapshot: func() api.Snapshot { return snapshot }, Recovery: func() api.RecoveryState { return api.RecoveryState{Fence: "fence"} }, Submit: func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
		submitted.Add(1)
		return api.Receipt{Outcome: "unknown"}, errors.New("lost receipt")
	}}
	b, err := Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID, b.state.BaseURL = "bot", "owner", defaultBase
	b.state.Inbox = []inbound{{ID: "weixin:bot:1", Text: "work", ContextToken: "ctx"}}
	b.dispatch(t.Context())
	b.dispatch(t.Context())
	if submitted.Load() != 1 || b.state.Inputs["weixin:bot:1"] != "unknown" {
		t.Fatalf("submit replay or wrong outcome: %d %#v", submitted.Load(), b.state.Inputs)
	}
	freshWindow(b, "weixin:bot:1")
	snapshot.CurrentTurn, snapshot.Phase = "turn", "completed"
	snapshot.Items = []api.Item{{ID: "answer", TurnKey: "turn", Kind: "assistant", Text: "reply", Status: "completed"}}
	p := &protocol{client: server.Client(), base: server.URL, token: "secret"}
	b.output(t.Context(), p)
	b.output(t.Context(), p)
	intent := b.state.Outputs["item:answer:0"]
	if sent.Load() != 1 || intent.State != "unknown" || intent.Attempts != 1 || intent.Reason != "http_error" || b.state.Window.Used != 1 {
		t.Fatalf("uncertain send was retried or uncounted: sends=%d intent=%#v window=%#v", sent.Load(), intent, b.state.Window)
	}
	reopened, err := Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	freshWindow(reopened, "weixin:bot:2")
	reopened.output(t.Context(), p)
	if sent.Load() != 1 {
		t.Fatal("unknown send replayed after restart or new ingress")
	}
}

func TestBusinessRejectionKeepsRedactedDiagnosticsAndWaitsForFreshIngress(t *testing.T) {
	var sent atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sent.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"ret":-2,"errcode":0,"errmsg":"rate limited: private data must not persist"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "t1", Phase: "completed", Items: []api.Item{{ID: "a1", TurnKey: "t1", Kind: "assistant", Text: "answer", Status: "completed"}}}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID = "bot", "owner"
	freshWindow(b, "in-1")
	p := &protocol{client: server.Client(), base: server.URL, token: "secret"}
	b.output(t.Context(), p)
	b.output(t.Context(), p)
	one := b.state.Outputs["item:a1:0"]
	if sent.Load() != 1 || one.State != "rejected" || one.ErrorClass != "rate_limited" || one.Ret == nil || *one.Ret != -2 || !b.state.Window.Exhausted || one.Bytes != len("answer") || one.WindowUsed != 1 {
		t.Fatalf("business rejection: sent=%d intent=%#v window=%#v", sent.Load(), one, b.state.Window)
	}
	body, err := os.ReadFile(b.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "private data") || strings.Contains(string(body), "answer") {
		t.Fatal("diagnostic ledger stored reply or raw errmsg")
	}
	freshWindow(b, "in-2")
	snapshot.CurrentTurn = "t2"
	snapshot.Items = append(snapshot.Items, api.Item{ID: "a2", TurnKey: "t2", Kind: "assistant", Text: "new answer", Status: "completed"})
	b.output(t.Context(), p)
	if sent.Load() != 2 || b.state.Outputs["item:a1:0"].State != "rejected" || b.state.Outputs["item:a2:0"].State != "accepted" {
		t.Fatal("fresh ingress replayed rejected output or blocked new final")
	}
}

func TestLongReplySendsEveryBoundedPartWhenSuccessOmitsRet(t *testing.T) {
	var delivered []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Msg struct {
				Items []messageItem `json:"item_list"`
			} `json:"msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Msg.Items) != 1 {
			t.Errorf("send body: %v", err)
			return
		}
		part := req.Msg.Items[0].Text.Text
		if !fitsText(part) {
			t.Errorf("oversize part: %d units, %d bytes", textUnits(part), len(part))
		}
		delivered = append(delivered, part)
		_, _ = w.Write([]byte(`{"errmsg":""}`))
	}))
	defer server.Close()
	answer := strings.Repeat("你好世界。", 500)
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "turn", Phase: "completed", Items: []api.Item{{ID: "long-answer", TurnKey: "turn", Kind: "assistant", Text: answer, Status: "completed"}}}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID, b.state.HasInput = "bot", "owner", true
	freshWindow(b, "prior-owner-input")
	p := &protocol{client: server.Client(), base: server.URL, token: "secret"}
	b.output(t.Context(), p)
	b.output(t.Context(), p)
	if len(delivered) < 2 || strings.Join(delivered, "") != answer || b.issue == "delivery_uncertain" || b.state.Window.Used != len(delivered) {
		t.Fatalf("reply was truncated, repeated, uncertain, or miscounted: parts=%d used=%d issue=%q", len(delivered), b.state.Window.Used, b.issue)
	}
	for i := range delivered {
		part := b.state.Outputs["item:long-answer:"+string(rune('0'+i))]
		if part.State != "accepted" || part.Reason != "http_success_no_ret" {
			t.Fatalf("part %d not accepted", i)
		}
	}
}

func TestStaleSessionPersistsCooldownWithoutDiscardingPairing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ret":-14,"errcode":-14}`))
	}))
	defer server.Close()
	root := t.TempDir()
	b, err := Open(root, Host{})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID, b.state.Enabled = "bot", "owner", true
	b.state.BaseURL = defaultBase
	if err := b.saveLocked(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		b.receive(ctx, &protocol{client: server.Client(), base: server.URL, token: "secret"})
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for b.Status().Issue != "session_cooldown" {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("cooldown not entered")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done
	reloaded, err := Open(root, Host{})
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.state.BotID != "bot" || reloaded.state.PauseUntil <= time.Now().UnixMilli() || reloaded.Status().Issue != "session_cooldown" {
		t.Fatalf("pairing or cooldown lost: bot=%q pause=%d issue=%q", reloaded.state.BotID, reloaded.state.PauseUntil, reloaded.Status().Issue)
	}
}

func TestQRVerificationReachesLocalConfirmation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/get_qrcode_status" || r.URL.Query().Get("qrcode") != "code" {
			t.Errorf("wrong QR status request")
		}
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"status":"need_verifycode"}`))
			return
		}
		if r.URL.Query().Get("verify_code") != "1234" {
			t.Errorf("verification code was not sent")
		}
		_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"secret","ilink_bot_id":"bot@im.wechat","ilink_user_id":"owner@im.wechat","baseurl":"https://ilinkai.weixin.qq.com"}`))
	}))
	defer server.Close()
	b, err := Open(t.TempDir(), Host{})
	if err != nil {
		t.Fatal(err)
	}
	b.qrCode = "code"
	b.qrImage = "data:image/png;base64,fixture"
	b.expires = time.Now().Add(time.Minute)
	b.verifyCh = make(chan string, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		b.pollPair(ctx, &protocol{client: server.Client(), base: server.URL}, "code", b.verifyCh, 0)
		close(done)
	}()
	until := time.After(2 * time.Second)
	for b.Status().Phase != "verify" {
		select {
		case <-until:
			t.Fatal("verify phase not reached")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, err := b.Verify("1234"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("confirmation not reached")
	}
	status := b.Status()
	if status.Phase != "confirm" || status.QRImage != "" || status.Owner == "" || status.Owner == "••••" || b.candidate.Token != "secret" {
		t.Fatalf("wrong confirmation state: %#v", status)
	}
}
