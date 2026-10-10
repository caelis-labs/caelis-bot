package weixin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestReceiveDurableOwnerOnlyAndNoDuplicate(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 1 {
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"next-cursor","msgs":[{"message_id":18446744073709551615,"from_user_id":"owner","to_user_id":"bot","message_type":1,"context_token":"ctx","item_list":[{"type":1,"text_item":{"text":"hello"}}]},{"message_id":"18446744073709551615","from_user_id":"owner","to_user_id":"bot","message_type":1,"item_list":[{"type":1,"text_item":{"text":"duplicate"}}]},{"message_id":"2","from_user_id":"stranger","to_user_id":"bot","message_type":1,"item_list":[{"type":1,"text_item":{"text":"intruder"}}]},{"message_id":"3","from_user_id":"owner","to_user_id":"bot","group_id":"group","message_type":1,"item_list":[{"type":1,"text_item":{"text":"group"}}]}]}`))
	}))
	defer server.Close()
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

func TestUnknownSubmitAndSendAreNeverReplayed(t *testing.T) {
	var submitted, sent atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sent.Add(1); w.WriteHeader(http.StatusGatewayTimeout) }))
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
	if sent.Load() != 1 || b.state.Outputs["item:answer:0"].State != "unknown" {
		t.Fatalf("send replay or wrong outcome: %d %#v", sent.Load(), b.state.Outputs)
	}
	body, err := os.ReadFile(b.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "reply") || strings.Contains(string(body), "work") {
		t.Fatalf("state included conversation content after processing")
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
