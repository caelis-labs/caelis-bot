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

func TestTextApprovalWorksWhileResidentSubmitIsGated(t *testing.T) {
	var sent atomic.Int32
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
	b, err := Open(root, Host{Snapshot: func() api.Snapshot { return snapshot }, Recovery: func() api.RecoveryState { return api.RecoveryState{InProgress: true} }, TextControl: control, Submit: func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
		t.Fatal("control used ordinary Submit")
		return api.Receipt{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID, b.state.Enabled = "bot", "owner", true
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	b.output(t.Context(), p)
	if len(texts) != 1 || !strings.Contains(texts[0], "选项 1：允许（范围：仅本次）\n/approve A1 1") {
		t.Fatalf("missing text card: %#v", texts)
	}
	b.state.Inbox = []inbound{{ID: "weixin:bot:1", Text: "/approve A1 1", ContextToken: "ctx"}}
	b.dispatch(t.Context(), p)
	b.dispatch(t.Context(), p)
	if sent.Load() != 1 || len(texts) != 2 || !strings.Contains(texts[1], "决定已提交") {
		t.Fatalf("decision/receipt: %d %#v", sent.Load(), texts)
	}
	snapshot.Approvals[0].Status = "resolved"
	b.output(t.Context(), p)
	if len(texts) != 3 || !strings.Contains(texts[2], "已处理") {
		t.Fatalf("terminal mirror: %#v", texts)
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
	b.state.Inputs["remote-wx"] = "accepted"
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
	b.mu.Unlock()
	if in.ID != "weixin:bot:18446744073709551615" || in.Text != "hello" || in.ContextToken != "ctx" {
		t.Fatalf("wrong inbox: %#v", in)
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

func TestUnknownSubmitIsNotReplayedAndSendHasThreeAttemptLimit(t *testing.T) {
	var submitted, sent atomic.Int32
	var clientIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Msg struct {
				ClientID string `json:"client_id"`
			} `json:"msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("send request: %v", err)
		}
		clientIDs = append(clientIDs, request.Msg.ClientID)
		sent.Add(1)
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer server.Close()
	snapshot := api.Snapshot{Connection: "ready"}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }, Recovery: func() api.RecoveryState { return api.RecoveryState{Fence: "fence"} }, Submit: func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
		submitted.Add(1)
		return api.Receipt{Outcome: "unknown"}, errors.New("lost receipt")
	}})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID = "bot"
	b.state.OwnerID = "owner"
	b.state.BaseURL = defaultBase
	b.state.Enabled = true
	b.state.Inbox = []inbound{{ID: "weixin:bot:1", Text: "work", ContextToken: "ctx"}}
	if err := b.saveLocked(); err != nil {
		t.Fatal(err)
	}
	b.dispatch(t.Context())
	b.dispatch(t.Context())
	if submitted.Load() != 1 || b.state.Inputs["weixin:bot:1"] != "unknown" {
		t.Fatalf("submit replay or wrong outcome: %d %#v", submitted.Load(), b.state.Inputs)
	}
	b.state.ContextToken = "ctx"
	snapshot.Items = []api.Item{{ID: "answer", Kind: "assistant", Text: "reply", Status: "completed"}}
	p := &protocol{client: server.Client(), base: server.URL, token: "secret"}
	b.output(t.Context(), p)
	b.output(t.Context(), p)
	if sent.Load() != 1 || b.issue != "delivery_retrying" {
		t.Fatalf("early retry: %d %q", sent.Load(), b.issue)
	}
	for attempt := 2; attempt <= 3; attempt++ {
		intent := b.state.Outputs["item:answer:0"]
		intent.NextRetryAt = time.Now().Add(-time.Second).UnixMilli()
		b.state.Outputs["item:answer:0"] = intent
		if err := b.saveLocked(); err != nil {
			t.Fatal(err)
		}
		b.output(t.Context(), p)
	}
	b.output(t.Context(), p)
	intent := b.state.Outputs["item:answer:0"]
	if sent.Load() != 3 || intent.State != "unknown" || intent.Attempts != 3 || intent.Reason != "http_error" || b.issue != "delivery_uncertain" {
		t.Fatalf("wrong bounded send outcome: %d %#v %q", sent.Load(), intent, b.issue)
	}
	if clientIDs[0] == "" || clientIDs[0] != clientIDs[1] || clientIDs[1] != clientIDs[2] {
		t.Fatal("retry changed client ID")
	}
	reopened, err := Open(strings.TrimSuffix(b.path, "/weixin.json"), b.host)
	if err != nil {
		t.Fatal(err)
	}
	reopened.output(t.Context(), p)
	if sent.Load() != 3 {
		t.Fatal("exhausted send replayed after restart")
	}
	legacy := reopened.state.Outputs["item:old:0"]
	legacy.State, legacy.ClientID = "unknown", "old-client"
	reopened.state.Outputs["item:old:0"] = legacy
	snapshot.Items = []api.Item{{ID: "old", Kind: "assistant", Text: "old reply", Status: "completed"}}
	reopened.output(t.Context(), p)
	if sent.Load() != 3 {
		t.Fatal("pre-policy unknown replayed")
	}
	if intent.Digest != textDigest("reply") {
		t.Fatal("send digest mismatch")
	}
	body, err := os.ReadFile(b.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "reply") || strings.Contains(string(body), "work") {
		t.Fatalf("state included conversation content after processing")
	}
}

func TestUnknownSendRetryAcceptsWithoutRepeatingOrChangingClientID(t *testing.T) {
	var sent atomic.Int32
	var clientIDs, contextTokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Msg struct {
				ClientID     string `json:"client_id"`
				ContextToken string `json:"context_token"`
			} `json:"msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("send request: %v", err)
		}
		clientIDs = append(clientIDs, request.Msg.ClientID)
		contextTokens = append(contextTokens, request.Msg.ContextToken)
		if sent.Add(1) == 1 {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()
	root := t.TempDir()
	snapshot := api.Snapshot{Connection: "ready", Items: []api.Item{{ID: "answer", Kind: "assistant", Text: "reply", Status: "completed"}}}
	host := Host{Snapshot: func() api.Snapshot { return snapshot }}
	b, err := Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID, b.state.ContextToken = "bot", "owner", "ctx"
	b.state.BaseURL = defaultBase
	p := &protocol{client: server.Client(), base: server.URL, token: "secret"}
	b.output(t.Context(), p)
	b.state.ContextToken = "newer-context"
	if err := b.saveLocked(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	reopened.output(t.Context(), p)
	if sent.Load() != 1 {
		t.Fatal("retry ignored persisted delay")
	}
	intent := reopened.state.Outputs["item:answer:0"]
	intent.NextRetryAt = time.Now().Add(-time.Second).UnixMilli()
	reopened.state.Outputs["item:answer:0"] = intent
	if err := reopened.saveLocked(); err != nil {
		t.Fatal(err)
	}
	reopened.output(t.Context(), p)
	reopened.output(t.Context(), p)
	if sent.Load() != 2 || clientIDs[0] == "" || clientIDs[0] != clientIDs[1] || contextTokens[0] != "ctx" || contextTokens[1] != "ctx" || reopened.state.Outputs["item:answer:0"].State != "accepted" || reopened.issue != "" {
		t.Fatalf("retry acceptance mismatch: sent=%d state=%#v issue=%q", sent.Load(), reopened.state.Outputs["item:answer:0"], reopened.issue)
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
	snapshot := api.Snapshot{Connection: "ready", Items: []api.Item{{ID: "long-answer", Kind: "assistant", Text: answer, Status: "completed"}}}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID, b.state.HasInput = "bot", "owner", true
	p := &protocol{client: server.Client(), base: server.URL, token: "secret"}
	b.output(t.Context(), p)
	b.output(t.Context(), p)
	if len(delivered) < 2 || strings.Join(delivered, "") != answer || b.issue == "delivery_uncertain" {
		t.Fatalf("reply was truncated, repeated, or uncertain: parts=%d issue=%q", len(delivered), b.issue)
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
