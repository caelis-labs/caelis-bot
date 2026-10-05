package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tg "github.com/mymmrac/telego"
)

func TestSDKUsesTelegramContractsAndSanitizesPrivateErrors(t *testing.T) {
	const fixtureToken = "123456:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	methods := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := filepath.Base(r.URL.Path)
		methods = append(methods, method)
		if !strings.HasPrefix(r.URL.Path, "/bot"+fixtureToken+"/") {
			t.Error("wrong SDK API target")
		}
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "getMe":
			io.WriteString(w, `{"ok":true,"result":{"id":123,"is_bot":true,"first_name":"fixture","username":"fixture_bot"}}`)
		case "getUpdates":
			var p tg.GetUpdatesParams
			json.NewDecoder(r.Body).Decode(&p)
			if p.Offset != 15 || p.Timeout != 25 {
				t.Error("polling cursor lost")
			}
			io.WriteString(w, `{"ok":true,"result":[{"update_id":15,"message":{"message_id":7,"from":{"id":20,"is_bot":false,"first_name":"Owner"},"chat":{"id":10,"type":"private"},"date":1,"text":"hi"}}]}`)
		case "sendMessage":
			var p tg.SendMessageParams
			json.NewDecoder(r.Body).Decode(&p)
			if p.ChatID.ID != 10 || p.Text != "plain **text**" || p.ParseMode != "" {
				t.Error("plaintext or chat contract lost")
			}
			io.WriteString(w, `{"ok":true,"result":{"message_id":101,"date":1,"chat":{"id":10,"type":"private"},"text":"plain **text**"}}`)
		case "editMessageText":
			io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`)
		case "sendDocument":
			if e := r.ParseMultipartForm(1 << 20); e != nil {
				t.Error(e)
			}
			file, head, e := r.FormFile("document")
			if e != nil {
				t.Error("SDK multipart missing", e)
				return
			}
			defer file.Close()
			data, _ := io.ReadAll(file)
			if head.Filename != "attachment.txt" || string(data) != "exact file bytes" {
				t.Error("file name or bytes changed")
			}
			io.WriteString(w, `{"ok":true,"result":{"message_id":102,"date":1,"chat":{"id":10,"type":"private"}}}`)
		default:
			io.WriteString(w, `{"ok":false,"error_code":401,"description":"private token and message must never escape"}`)
		}
	}))
	defer server.Close()
	bot, e := tg.NewBot(fixtureToken, tg.WithHTTPClient(server.Client()), tg.WithAPIServer(server.URL), tg.WithDiscardLogger())
	if e != nil {
		t.Fatal("fixture SDK configuration failed")
	}
	c := &sdkClient{bot: bot, http: server.Client()}
	ctx := context.Background()
	if u, e := c.Me(ctx); e != nil || u.Username != "fixture_bot" {
		t.Fatal("SDK getMe failed")
	}
	if v, e := c.Updates(ctx, 15); e != nil || len(v) != 1 || v[0].Message.Text != "hi" {
		t.Fatal("SDK updates failed")
	}
	if id, e := c.Send(ctx, 10, "plain **text**", nil); e != nil || id != 101 {
		t.Fatal("SDK message failed")
	}
	if e := c.Edit(ctx, 10, 101, "plain **text**"); e != nil {
		t.Fatal("an already applied edit did not reconcile")
	}
	path := filepath.Join(t.TempDir(), "attachment.txt")
	os.WriteFile(path, []byte("exact file bytes"), 0600)
	if e := c.Document(ctx, 10, path); e != nil {
		t.Fatal("SDK document failed")
	}
	if _, e := c.Webhook(ctx); e == nil || e.Error() != "invalid_token" {
		t.Fatal("private API response escaped sanitized error")
	}
	if len(methods) != 6 {
		t.Fatal("unexpected SDK calls")
	}
}
