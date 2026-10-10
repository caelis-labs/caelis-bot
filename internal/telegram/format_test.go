package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	tg "github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
)

func TestAmbiguousBotAPI400DiagnosticUsesOnlySafeClass(t *testing.T) {
	for _, tc := range []struct{ description, want string }{
		{"Bad Request: METHOD_NOT_AVAILABLE", "telegram_code"},
		{"Bad Request: method is not supported", "method_unsupported"},
		{"Bad Request: invalid private payload 123:SECRET", "invalid"},
		{"Bad Request: private payload 123:SECRET", "other"},
	} {
		var records []diagnosticlog.Record
		ctx := withDeliveryTrace(t.Context(), func(r diagnosticlog.Record) { records = append(records, r) }, "item:test", 0)
		err := safeMethodError("sendMessage", &telegoapi.Error{ErrorCode: 400, Description: tc.description})
		reportDeliveryAttempt(ctx, "sendMessage", "html", err, false)
		if len(records) != 1 || records[0].Reason != "api_code=400 category=api_rejected detail="+tc.want || strings.Contains(records[0].Reason, "SECRET") {
			t.Fatalf("unsafe or missing diagnostic class: %#v", records)
		}
	}
}

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

func TestFormattedMessageProtocolSendAndEdit(t *testing.T) {
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
	if fmt.Sprint(methods) != "[sendRichMessage editMessageText]" {
		t.Fatalf("methods: %v", methods)
	}
	rich, ok := requests[0]["rich_message"].(map[string]any)
	if !ok || rich["markdown"] != message.Markdown || requests[0]["parse_mode"] != nil {
		t.Fatalf("native rich send: %v", requests[0])
	}
	rich, ok = requests[1]["rich_message"].(map[string]any)
	if requests[1]["message_id"] != float64(77) || !ok || rich["markdown"] != message.Markdown || requests[1]["parse_mode"] != nil {
		t.Fatalf("native rich edit payload: %v", requests[1])
	}
	for _, request := range requests {
		if _, exists := request["reply_markup"]; exists {
			t.Fatalf("no-keyboard request encoded null reply_markup: %#v", request)
		}
	}
}

func TestRichMarkdownKeepsNumberedReferencesLiteral(t *testing.T) {
	source := "#147 已合并。#148 也完成\n#123 issue\n\n# 标题\n\n> #456 引用\n- #789 列表\n\n```md\n#321 code\n```\n\n[link](https://example.com/#123) and `#234`"
	want := "\\#147 已合并。#148 也完成\n\\#123 issue\n\n# 标题\n\n> \\#456 引用\n- \\#789 列表\n\n```md\n#321 code\n```\n\n[link](https://example.com/#123) and `#234`"
	if got := richMarkdown(source); got != want {
		t.Fatalf("rich Markdown mismatch:\n got: %q\nwant: %q", got, want)
	}
	if got := telegramHTML(source); !strings.Contains(got, "#147 已合并。#148 也完成") || !strings.Contains(got, "<b>标题</b>") {
		t.Fatalf("CommonMark HTML fallback changed text or heading: %q", got)
	}
	var payloads []map[string]any
	client := testSDK(t, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, request)
		reply(w, 200, `{"ok":true,"result":{"message_id":77,"date":0,"chat":{"id":123,"type":"private"}}}`)
	})
	message := outgoingText{Text: source, Markdown: source}
	if _, err := client.Send(t.Context(), 123, message, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.Edit(t.Context(), 123, 77, message, nil); err != nil {
		t.Fatal(err)
	}
	for _, request := range payloads {
		rich, ok := request["rich_message"].(map[string]any)
		if !ok || rich["markdown"] != want {
			t.Fatalf("send/edit did not share safe rich Markdown: %#v", request)
		}
	}
}

func TestFormattedUnknownDeliveryNeverFallsBack(t *testing.T) {
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

func TestRichPrimaryKeepsKeyboard(t *testing.T) {
	var methods []string
	var requests []map[string]any
	client := testSDK(t, func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		methods = append(methods, method)
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
		reply(w, 200, `{"ok":true,"result":{"message_id":81,"date":0,"chat":{"id":123,"type":"private"}}}`)
	})
	keys := &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{{{Text: "Allow once", CallbackData: "choice"}}}}
	message := outgoingText{Text: "**original** <&>", Markdown: "**original** <&>"}
	id, err := client.Send(t.Context(), 123, message, keys)
	if err != nil || id != 81 || fmt.Sprint(methods) != "[sendRichMessage]" {
		t.Fatalf("rich send failed: id=%d err=%v methods=%v", id, err, methods)
	}
	rich, ok := requests[0]["rich_message"].(map[string]any)
	if !ok || rich["markdown"] != message.Markdown || requests[0]["reply_markup"] == nil {
		t.Fatalf("rich send lost Markdown source or keyboard: %#v", requests)
	}
}

func TestHTMLFormatRejectionFallsBackToExactPlainSource(t *testing.T) {
	var methods []string
	var requests []map[string]any
	client := testSDK(t, func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		methods = append(methods, method)
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
		switch len(methods) {
		case 1, 2:
			reply(w, 400, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`)
		default:
			reply(w, 200, `{"ok":true,"result":{"message_id":82,"date":0,"chat":{"id":123,"type":"private"}}}`)
		}
	})
	message := outgoingText{Text: "# Source\n**literal** & A_B", Markdown: "# Source\n**literal** & A_B"}
	id, err := client.Send(t.Context(), 123, message, nil)
	if err != nil || id != 82 || fmt.Sprint(methods) != "[sendRichMessage sendMessage sendMessage]" {
		t.Fatalf("definite format rejections did not reach plain fallback: id=%d err=%v methods=%v", id, err, methods)
	}
	if requests[0]["rich_message"] == nil || requests[1]["parse_mode"] != "HTML" || requests[2]["text"] != message.Text || requests[2]["parse_mode"] != nil {
		t.Fatalf("plain fallback changed source or retained formatting: %#v", requests)
	}
	for _, request := range requests {
		if _, exists := request["reply_markup"]; exists {
			t.Fatalf("fallback encoded null reply_markup: %#v", request)
		}
	}
}

func TestRichMethodMissingFallsBackToHTML(t *testing.T) {
	var methods []string
	client := testSDK(t, func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		methods = append(methods, method)
		if method == "sendRichMessage" {
			reply(w, 404, `{"ok":false,"error_code":404,"description":"Method sendRichMessage not found"}`)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["parse_mode"] != "HTML" {
			t.Fatalf("fallback did not use supported HTML: %#v", request)
		}
		reply(w, 200, `{"ok":true,"result":{"message_id":83,"date":0,"chat":{"id":123,"type":"private"}}}`)
	})
	id, err := client.Send(t.Context(), 123, outgoingText{Text: "**bold**", Markdown: "**bold**"}, nil)
	if err != nil || id != 83 || fmt.Sprint(methods) != "[sendRichMessage sendMessage]" {
		t.Fatalf("missing rich method fallback: id=%d err=%v methods=%v", id, err, methods)
	}
}

func TestExplicitRichParseRejectionFallsBackToHTML(t *testing.T) {
	var methods []string
	client := testSDK(t, func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		methods = append(methods, method)
		if method == "sendRichMessage" {
			reply(w, 400, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse rich message"}`)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["parse_mode"] != "HTML" {
			t.Fatalf("rich parse rejection did not fall back to HTML: %#v", request)
		}
		reply(w, 200, `{"ok":true,"result":{"message_id":83,"date":0,"chat":{"id":123,"type":"private"}}}`)
	})
	id, err := client.Send(t.Context(), 123, outgoingText{Text: "**bold**", Markdown: "**bold**"}, nil)
	if err != nil || id != 83 || fmt.Sprint(methods) != "[sendRichMessage sendMessage]" {
		t.Fatalf("explicit rich parse rejection did not use one HTML fallback: id=%d err=%v methods=%v", id, err, methods)
	}
}

func TestHTMLOtherRejectionsAndUnknownNeverFallBack(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
		wantCode       int
		wantCategory   string
	}{
		{"bad_chat", `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, 400, 400, "api_rejected"},
		{"bad_keyboard", `{"ok":false,"error_code":400,"description":"Bad Request: can't parse reply markup"}`, 400, 400, "api_rejected"},
		{"invalid_token", `{"ok":false,"error_code":401,"description":"Unauthorized"}`, 401, 401, "authentication"},
		{"blocked", `{"ok":false,"error_code":403,"description":"Forbidden"}`, 403, 403, "forbidden"},
		{"missing_chat", `{"ok":false,"error_code":404,"description":"chat not found"}`, 404, 404, "api_rejected"},
		{"conflict", `{"ok":false,"error_code":409,"description":"Conflict"}`, 409, 409, "conflict"},
		{"rate_limit", `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":3}}`, 429, 429, "rate_limited"},
		{"server_error", `{"ok":false,"error_code":502,"description":"Bad Gateway"}`, 502, 0, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := testSDK(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				reply(w, tc.status, tc.response)
			})
			_, err := client.Send(t.Context(), 123, outgoingText{Text: "**text**", Markdown: "**text**"}, nil)
			var transport *transportError
			if !errors.As(err, &transport) || transport.code != tc.wantCode || transport.category != tc.wantCategory || calls != 1 {
				t.Fatalf("rejection replayed or misclassified: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestHTMLEditFallbackKeepsOriginalMessageAndKeyboard(t *testing.T) {
	var requests []map[string]any
	client := testSDK(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/editMessageText") {
			t.Fatal("edit fallback created a new message")
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
		switch len(requests) {
		case 1, 2:
			reply(w, 400, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`)
		default:
			reply(w, 200, `{"ok":true,"result":{"message_id":77,"date":0,"chat":{"id":123,"type":"private"}}}`)
		}
	})
	keys := &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{{{Text: "Allow once", CallbackData: "choice"}}}}
	message := outgoingText{Text: "**updated** source", Markdown: "**updated** source"}
	if err := client.Edit(t.Context(), 123, 77, message, keys); err != nil || len(requests) != 3 {
		t.Fatalf("edit fallback failed or sent a new message: err=%v requests=%d", err, len(requests))
	}
	for _, request := range requests {
		if request["message_id"] != float64(77) || request["reply_markup"] == nil {
			t.Fatalf("edit changed original ID or keyboard: %#v", request)
		}
	}
	if requests[0]["rich_message"] == nil || requests[1]["parse_mode"] != "HTML" || requests[2]["text"] != message.Text || requests[2]["parse_mode"] != nil {
		t.Fatalf("plain edit lost exact source: %#v", requests)
	}
}

func TestHTMLTimeoutDoesNotTryAnotherSendMethod(t *testing.T) {
	calls := 0
	httpClient := &http.Client{Transport: fixtureTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, context.DeadlineExceeded
	})}
	bot, err := tg.NewBot("123:"+strings.Repeat("a", 35), tg.WithAPIServer("https://telegram.fixture.invalid"), tg.WithHTTPClient(httpClient), tg.WithDiscardLogger())
	if err != nil {
		t.Fatal(err)
	}
	client := &sdkClient{bot: bot, http: httpClient}
	_, err = client.Send(t.Context(), 123, outgoingText{Text: "**text**", Markdown: "**text**"}, nil)
	var transport *transportError
	if !errors.As(err, &transport) || transport.code != 0 || transport.category != "unknown" || calls != 1 {
		t.Fatalf("timeout changed format or repeated an unknown send: calls=%d err=%v", calls, err)
	}
}

func TestBridgeHTMLFallbackKeepsOriginalDeliveryAndSafeDiagnostics(t *testing.T) {
	var records []diagnosticlog.Record
	b, _ := testBridge(t, Host{Diagnostics: func(record diagnosticlog.Record) { records = append(records, record) }})
	paired(b)
	var methods []string
	sendCount := 0
	client := testSDK(t, func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		methods = append(methods, method)
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		switch method {
		case "editMessageText":
			if request["rich_message"] != nil {
				reply(w, 400, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities PRIVATE_MARKER"}`)
			} else {
				if request["message_id"] != float64(52) || request["parse_mode"] != "HTML" {
					t.Error("fallback edit lost original target or format")
				}
				reply(w, 200, `{"ok":true,"result":{"message_id":52,"date":0,"chat":{"id":10,"type":"private"}}}`)
			}
		case "sendRichMessage":
			if request["rich_message"] == nil {
				t.Error("assistant did not try native rich Markdown first")
			}
			reply(w, 400, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities PRIVATE_MARKER"}`)
		case "sendMessage":
			sendCount++
			if sendCount == 2 {
				if request["parse_mode"] != "HTML" {
					t.Error("assistant did not try HTML after definite rich rejection")
				}
			}
			id := 51
			if sendCount == 2 {
				id = 52
			}
			reply(w, 200, fmt.Sprintf(`{"ok":true,"result":{"message_id":%d,"date":0,"chat":{"id":10,"type":"private"}}}`, id))
		default:
			t.Error("unexpected method", method)
		}
	})
	items := []api.Item{
		{ID: "mac", Kind: "user", RequestID: "desktop:one", Text: "Mac original"},
		{ID: "reply", Kind: "assistant", Text: "**Bot reply**"},
	}
	b.mirror(t.Context(), client, api.Snapshot{Items: items})
	if record := b.state.Messages["item:reply"]; len(record.IDs) != 1 || record.IDs[0] != 52 {
		t.Fatalf("HTML fallback failed to retain the actual Telegram ID: %#v", record)
	}
	items[1].Text = "**Bot reply**, continued"
	b.mirror(t.Context(), client, api.Snapshot{Items: items})
	b.mirror(t.Context(), client, api.Snapshot{Items: items})
	if got := fmt.Sprint(methods); got != "[sendMessage sendRichMessage sendMessage editMessageText editMessageText]" {
		t.Fatalf("Mac mirror, Bot fallback, or original-ID streaming edit changed: %s", got)
	}
	var failureCodes []string
	for _, record := range records {
		if record.Code != "delivery_attempt" {
			continue
		}
		if record.Fingerprint != digest("item:reply") || record.Sequence != 1 || record.Method == "" {
			t.Fatalf("diagnostic lost original assistant delivery correlation: %#v", record)
		}
		if record.Level == "warning" {
			failureCodes = append(failureCodes, record.Reason)
		}
	}
	if fmt.Sprint(failureCodes) != "[api_code=400 category=format_rejected api_code=400 category=format_rejected]" {
		t.Fatalf("safe API rejection classification missing: %#v", failureCodes)
	}
	encoded, _ := json.Marshal(records)
	if strings.Contains(string(encoded), "Bot reply") || strings.Contains(string(encoded), "desktop:one") || strings.Contains(string(encoded), "123:aaaa") || strings.Contains(string(encoded), "PRIVATE_MARKER") || strings.Contains(string(encoded), `"chatId"`) {
		t.Fatal("diagnostic leaked message, token, or chat identity")
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

func TestTelegramHTMLPreservesLiteralTags(t *testing.T) {
	markdown := "**Bold** `code_<&>` <tag> & A_B\n\n<div>block & text</div>\n"
	if !hasRawHTML(markdown) || hasRawHTML("**Bold** `code_<tag>` [link](https://example.com)") {
		t.Fatal("raw HTML detection changed Markdown or code semantics")
	}
	formatted := telegramHTML(markdown)
	for _, want := range []string{"<b>Bold</b>", "<code>code_&lt;&amp;&gt;</code>", "&lt;tag&gt;", "&lt;div&gt;block &amp; text&lt;/div&gt;"} {
		if !strings.Contains(formatted, want) {
			t.Errorf("missing literal text %q in %q", want, formatted)
		}
	}
	if strings.Contains(formatted, "<tag>") || strings.Contains(formatted, "<div>") {
		t.Fatalf("raw model HTML became markup: %q", formatted)
	}
}

func TestRawHTMLSourceUsesEscapedHTMLSendAndEdit(t *testing.T) {
	var methods []string
	var requests []map[string]any
	client := testSDK(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:])
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
		reply(w, 200, `{"ok":true,"result":{"message_id":84,"date":0,"chat":{"id":123,"type":"private"}}}`)
	})
	message := outgoingText{Text: "**Bold** <tag> & A_B", Markdown: "**Bold** <tag> & A_B"}
	id, err := client.Send(t.Context(), 123, message, nil)
	if err != nil || id != 84 {
		t.Fatalf("send literal tag: id=%d err=%v", id, err)
	}
	if err := client.Edit(t.Context(), 123, id, message, nil); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(methods) != "[sendMessage editMessageText]" {
		t.Fatalf("raw HTML source reached rich parser: %v", methods)
	}
	for _, request := range requests {
		formatted, _ := request["text"].(string)
		if request["parse_mode"] != "HTML" || request["rich_message"] != nil || !strings.Contains(formatted, "&lt;tag&gt;") || !strings.Contains(formatted, "<b>Bold</b>") {
			t.Fatalf("literal tag or Markdown formatting lost: %#v", request)
		}
	}
}

func TestPlainEditOmitsAbsentReplyMarkup(t *testing.T) {
	var request map[string]any
	client := testSDK(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/editMessageText") {
			t.Fatal("plain edit used wrong method")
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		reply(w, 200, `{"ok":true,"result":{"message_id":77,"date":0,"chat":{"id":123,"type":"private"}}}`)
	})
	if err := client.Edit(t.Context(), 123, 77, outgoingText{Text: "plain edit"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, exists := request["reply_markup"]; exists {
		t.Fatalf("plain edit encoded absent keyboard: %#v", request)
	}
}
