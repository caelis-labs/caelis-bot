package weixin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublishedTextProtocolFixture(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("iLink-App-Id") != "bot" || r.Header.Get("iLink-App-ClientVersion") != clientVersion {
			t.Errorf("common headers missing")
		}
		switch r.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			if r.Method != "POST" || r.URL.Query().Get("bot_type") != "3" || r.Header.Get("Authorization") != "" {
				t.Errorf("wrong QR request")
			}
			_, _ = w.Write([]byte(`{"qrcode":"opaque-code","qrcode_img_content":"https://example.invalid/pair"}`))
		case "/ilink/bot/get_qrcode_status":
			if r.Method != "GET" || r.URL.Query().Get("qrcode") != "opaque-code" || r.URL.Query().Get("verify_code") != "1234" {
				t.Errorf("wrong status request")
			}
			_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"secret","ilink_bot_id":"bot@im.wechat","ilink_user_id":"owner@im.wechat","baseurl":"https://ilinkai.weixin.qq.com"}`))
		case "/ilink/bot/getupdates":
			if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("AuthorizationType") != "ilink_bot_token" || r.Header.Get("X-WECHAT-UIN") == "" {
				t.Errorf("auth headers missing")
			}
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["get_updates_buf"] != "cursor" || req["base_info"].(map[string]any)["channel_version"] != channelVersion {
				t.Errorf("wrong update body: %#v", req)
			}
			_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"next","msgs":[{"message_id":18446744073709551615,"from_user_id":"owner@im.wechat","to_user_id":"bot@im.wechat","message_type":1,"context_token":"ctx","item_list":[{"type":1,"text_item":{"text":"hello"}}]}]}`))
		case "/ilink/bot/sendmessage":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			msg := req["msg"].(map[string]any)
			if msg["client_id"] != "stable-id" || msg["context_token"] != "ctx" || msg["to_user_id"] != "owner@im.wechat" || msg["message_state"] != float64(2) {
				t.Errorf("wrong send body: %#v", msg)
			}
			_, _ = w.Write([]byte(`{"ret":0,"errmsg":""}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	p := &protocol{client: server.Client(), base: server.URL, token: "secret"}
	qr, err := p.qr(t.Context())
	if err != nil || qr.Code != "opaque-code" {
		t.Fatalf("qr: %#v %v", qr, err)
	}
	status, err := p.qrStatus(t.Context(), qr.Code, "1234")
	if err != nil || status.UserID != "owner@im.wechat" {
		t.Fatalf("status: %#v %v", status, err)
	}
	update, err := p.getUpdates(t.Context(), "cursor")
	if err != nil || update.Cursor != "next" || len(update.Messages) != 1 || string(update.Messages[0].MessageID) != "18446744073709551615" {
		t.Fatalf("updates: %#v %v", update, err)
	}
	result, err := p.send(t.Context(), status.UserID, "ctx", "stable-id", "reply")
	if err != nil || result.Ret == nil || *result.Ret != 0 {
		t.Fatalf("send: %#v %v", result, err)
	}
	if len(paths) != 4 {
		t.Fatalf("paths: %#v", paths)
	}
}

func TestRejectUntrustedBase(t *testing.T) {
	for _, raw := range []string{"http://ilinkai.weixin.qq.com", "https://ilinkai.weixin.qq.com.evil.test", "https://user@ilinkai.weixin.qq.com", "https://ilinkai.weixin.qq.com:443", "https://ilinkai.weixin.qq.com/path"} {
		if _, err := safeBase(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	if got, err := safeBase(defaultBase); err != nil || !strings.EqualFold(got, defaultBase) {
		t.Fatalf("default: %q %v", got, err)
	}
}

func TestNumericAndStringMessageIDs(t *testing.T) {
	for _, raw := range []string{`18446744073709551615`, `"18446744073709551615"`} {
		var id wireID
		if err := json.Unmarshal([]byte(raw), &id); err != nil || string(id) != "18446744073709551615" {
			t.Fatalf("%s: %q %v", raw, id, err)
		}
	}
}
