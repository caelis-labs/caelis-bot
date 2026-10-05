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

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSDKUsesTelegramContractsAndSanitizesPrivateErrors(t *testing.T) {
	const fixtureToken = "123456:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	methods := []string{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			if p.ChatID.ID != 10 || p.ParseMode != "" {
				t.Error("plaintext or chat contract lost")
			}
			if p.Text == "plain **text**" {
				if len(p.Entities) != 0 {
					t.Error("plain message unexpectedly formatted")
				}
			} else {
				assertSDKRoleEntities(t, p.Text, p.Entities)
			}
			io.WriteString(w, `{"ok":true,"result":{"message_id":101,"date":1,"chat":{"id":10,"type":"private"},"text":"plain **text**"}}`)
		case "editMessageText":
			var p tg.EditMessageTextParams
			json.NewDecoder(r.Body).Decode(&p)
			if p.Text == "plain **text**" {
				if len(p.Entities) != 0 || p.ParseMode != "" {
					t.Error("plain assistant edit leaked display entities")
				}
				io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`)
			} else {
				if p.ChatID.ID != 10 || p.MessageID != 101 || p.ParseMode != "" {
					t.Error("role edit target or parse mode changed")
				}
				assertSDKRoleEntities(t, p.Text, p.Entities)
				io.WriteString(w, `{"ok":true,"result":{"message_id":101,"date":1,"chat":{"id":10,"type":"private"},"text":"updated"}}`)
			}
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
	})
	fixtureHTTP := &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	bot, e := tg.NewBot(fixtureToken, tg.WithHTTPClient(fixtureHTTP), tg.WithAPIServer("https://telegram.fixture.invalid"), tg.WithDiscardLogger())
	if e != nil {
		t.Fatal("fixture SDK configuration failed")
	}
	c := &sdkClient{bot: bot, http: fixtureHTTP}
	ctx := context.Background()
	if u, e := c.Me(ctx); e != nil || u.Username != "fixture_bot" {
		t.Fatal("SDK getMe failed")
	}
	if v, e := c.Updates(ctx, 15); e != nil || len(v) != 1 || v[0].Message.Text != "hi" {
		t.Fatal("SDK updates failed")
	}
	if id, e := c.Send(ctx, 10, plainText("plain **text**"), nil); e != nil || id != 101 {
		t.Fatal("SDK message failed")
	}
	if e := c.Edit(ctx, 10, 101, plainText("plain **text**")); e != nil {
		t.Fatal("an already applied edit did not reconcile")
	}
	role := macUserText("你 · 来自 Mac", "🦉\n`code` & https://example.com")[0]
	if _, e := c.Send(ctx, 10, role, nil); e != nil {
		t.Fatal("SDK role message failed", e)
	}
	if e := c.Edit(ctx, 10, 101, role); e != nil {
		t.Fatal("SDK role edit failed", e)
	}
	path := filepath.Join(t.TempDir(), "attachment.txt")
	os.WriteFile(path, []byte("exact file bytes"), 0600)
	if e := c.Document(ctx, 10, path); e != nil {
		t.Fatal("SDK document failed")
	}
	if _, e := c.Webhook(ctx); e == nil || e.Error() != "invalid_token" {
		t.Fatal("private API response escaped sanitized error")
	}
	if len(methods) != 8 {
		t.Fatal("unexpected SDK calls")
	}
}

func assertSDKRoleEntities(t *testing.T, text string, entities []tg.MessageEntity) {
	t.Helper()
	title := "你 · 来自 Mac"
	body := "🦉\n`code` & https://example.com"
	if text != title+"\n"+body || len(entities) != 2 {
		t.Errorf("SDK lost role text or entities: %q %#v", text, entities)
		return
	}
	if entities[0].Type != tg.EntityTypeBold || entities[0].Offset != 0 || entities[0].Length != utf16Length(title) ||
		entities[1].Type != tg.EntityTypeBlockquote || entities[1].Offset != utf16Length(title+"\n") || entities[1].Length != utf16Length(body) {
		t.Errorf("SDK changed UTF-16 role entities: %#v", entities)
	}
}
