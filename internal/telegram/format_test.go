package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tg "github.com/mymmrac/telego"
)

func testSDK(t *testing.T, handler http.HandlerFunc) *sdkClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	bot, err := tg.NewBot("123:"+strings.Repeat("a", 35), tg.WithAPIServer(server.URL), tg.WithHTTPClient(server.Client()), tg.WithDiscardLogger())
	if err != nil {
		t.Fatal(err)
	}
	return &sdkClient{bot: bot, http: server.Client()}
}

func reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func TestRichMessageProtocolSendEditAndFallback(t *testing.T) {
	var methods []string
	var requests []map[string]any
	client := testSDK(t, func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		methods = append(methods, method)
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		requests = append(requests, request)
		if len(methods) == 1 {
			reply(w, 400, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse rich message"}`)
			return
		}
		reply(w, 200, `{"ok":true,"result":{"message_id":77,"date":0,"chat":{"id":123,"type":"private"}}}`)
	})
	message := outgoingText{Text: "# Heading\n**bold** and [link](https://example.com)", Markdown: "# Heading\n**bold** and [link](https://example.com)"}
	id, err := client.Send(context.Background(), 123, message, nil)
	if err != nil || id != 77 {
		t.Fatalf("send: id=%d err=%v", id, err)
	}
	if err := client.Edit(context.Background(), 123, id, message, nil); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(methods) != "[sendRichMessage sendMessage editMessageText]" {
		t.Fatalf("methods: %v", methods)
	}
	if rich := requests[0]["rich_message"].(map[string]any); rich["markdown"] != message.Markdown {
		t.Fatalf("rich payload: %v", rich)
	}
	if requests[1]["parse_mode"] != "HTML" || !strings.Contains(requests[1]["text"].(string), "<b>Heading</b>") {
		t.Fatalf("HTML fallback: %v", requests[1])
	}
	if requests[2]["message_id"] != float64(77) || requests[2]["rich_message"] == nil {
		t.Fatalf("edit payload: %v", requests[2])
	}
}

func TestRichUnknownDeliveryNeverFallsBack(t *testing.T) {
	calls := 0
	client := testSDK(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(502)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":502,"description":"bad gateway"}`))
	})
	_, err := client.Send(context.Background(), 123, outgoingText{Text: "**hello**", Markdown: "**hello**"}, nil)
	if err == nil || calls != 1 {
		t.Fatalf("unknown outcome retried: calls=%d err=%v", calls, err)
	}
}

func TestTelegramHTMLAndMarkdownChunks(t *testing.T) {
	formatted := telegramHTML("# Heading\n**bold** *italic* [link](https://example.com/?a=1&b=2) `code`\n\n- item\n\n```go\nfmt.Println(1)\n```\n\n| A | B |\n|---|---|\n| 1 | 2 |")
	for _, want := range []string{"<b>Heading</b>", "<b>bold</b>", "<i>italic</i>", "https://example.com/?a=1&amp;b=2", "<code>code</code>", "<pre>", "A | B"} {
		if !strings.Contains(formatted, want) {
			t.Errorf("missing %q in %s", want, formatted)
		}
	}
	body := "```go\n" + strings.Repeat("line of code\n", 400) + "```\n"
	parts := assistantMessages(body)
	if len(parts) < 2 {
		t.Fatal("long fenced block did not split")
	}
	var restored strings.Builder
	for _, part := range parts {
		if utf16Length(part.Markdown) > 4000 {
			t.Fatalf("rich part too long: %d", utf16Length(part.Markdown))
		}
		restored.WriteString(part.Text)
	}
	if restored.String() != body {
		t.Fatal("source changed during splitting")
	}
}
