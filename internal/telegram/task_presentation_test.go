package telegram

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	tg "github.com/mymmrac/telego"
)

func taskItem(title string) api.Item {
	return api.Item{ID: "task-start:original", Kind: "assistant", Text: title, Status: "completed", Task: &api.TaskPresentation{ID: "original", Title: title, Status: "working"}}
}

func TestTaskTitleUsesLiteralTextAndNativeUnderline(t *testing.T) {
	title := "😀 对齐 _ACP_ **SDK** <tag> & 发布"
	item := taskItem(title)
	b, c := testBridge(t, Host{})
	paired(b)
	b.mirror(t.Context(), c, api.Snapshot{Items: []api.Item{item}})
	if len(c.messages) != 1 {
		t.Fatalf("task deliveries: %d", len(c.messages))
	}
	message := c.messages[0]
	want := []tg.MessageEntity{{Type: tg.EntityTypeUnderline, Offset: 0, Length: utf16Length(title)}}
	if message.Text != title || message.Markdown != "" || !reflect.DeepEqual(message.Entities, want) {
		t.Fatalf("task format: %#v", message)
	}
	// Receipt identity survives polling, status changes and a new formatter.
	prior := b.state.Messages[itemKey(item)]
	item.Task.Status = "completed"
	b.mirror(t.Context(), c, api.Snapshot{Items: []api.Item{item}})
	if c.sends != 1 || c.edits != 0 || !reflect.DeepEqual(prior, b.state.Messages[itemKey(item)]) {
		t.Fatal("task status/style redelivered a retained notice")
	}
}

func TestTaskTitlePreservesEveryOriginalDeliveryDisposition(t *testing.T) {
	for _, id := range []int{41, -1, -2} {
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			item := taskItem("Original task")
			b, c := testBridge(t, Host{})
			paired(b)
			for _, hash := range []string{digest(item.Text), "older-projection-hash"} {
				prior := delivery{IDs: []int{id}, Hashes: []string{hash}}
				b.state.Messages[itemKey(item)] = prior
				b.mirror(t.Context(), c, api.Snapshot{Items: []api.Item{item}})
				if c.sends+c.edits+c.markupEdits != 0 || !reflect.DeepEqual(prior, b.state.Messages[itemKey(item)]) {
					t.Fatal("restyling changed the original receipt")
				}
			}
		})
	}
}

func TestTaskUnderlineSerializesThroughExistingSDKWithoutMarkup(t *testing.T) {
	const title = "😀 _ACP_ <SDK> & 发布"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Errorf("unexpected method %s", r.URL.Path)
		}
		var request tg.SendMessageParams
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Text != title || request.ParseMode != "" || len(request.Entities) != 1 || request.Entities[0].Type != tg.EntityTypeUnderline || request.Entities[0].Length != utf16Length(title) {
			t.Errorf("serialized task: %#v", request)
		}
		io.WriteString(w, `{"ok":true,"result":{"message_id":77,"date":1,"chat":{"id":10,"type":"private"}}}`)
	}))
	defer server.Close()
	bot, err := tg.NewBot("123456:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", tg.WithAPIServer(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := testBridge(t, Host{})
	paired(b)
	b.sendTaskText(t.Context(), &sdkClient{bot: bot}, "task:original", 10, title, title)
	if got := b.state.Messages["task:original"].IDs; len(got) != 1 || got[0] != 77 {
		t.Fatalf("lost send receipt: %v", got)
	}
}

func TestTaskUnderlineKeepsPrefixAndSplitTextOffsets(t *testing.T) {
	title := "😀 标题"
	body := strings.Repeat("a", 3998) + title
	b, c := testBridge(t, Host{})
	b.sendTaskText(t.Context(), c, "split", 10, body, title)
	var text strings.Builder
	units := 0
	for _, message := range c.messages {
		text.WriteString(message.Text)
		for _, entity := range message.Entities {
			units += entity.Length
			if entity.Offset+entity.Length > utf16Length(message.Text) {
				t.Fatal("entity crosses chunk")
			}
		}
	}
	if text.String() != body || units != utf16Length(title) {
		t.Fatal("chunking lost task text or underline")
	}
}

func TestTaskTitleCompletesUnsentPartsUsingOriginalFormatter(t *testing.T) {
	body := strings.Repeat("字", 4100)
	for _, legacy := range []bool{true, false} {
		t.Run(strconv.FormatBool(legacy), func(t *testing.T) {
			b, c := testBridge(t, Host{})
			messages := taskOutboundMessages(body, body)
			if legacy {
				messages = assistantOutboundMessages(body)
			}
			if len(messages) != 2 {
				t.Fatal("fixture must span two parts")
			}
			firstHash := outgoingDigest(messages[0])
			b.state.Messages["partial-task"] = delivery{IDs: []int{77}, Hashes: []string{firstHash}}
			b.sendTaskText(t.Context(), c, "partial-task", 10, body, body)
			if c.sends != 1 || c.edits != 0 || len(c.messages) != 1 || !reflect.DeepEqual(c.messages[0], messages[1]) {
				t.Fatal("partial task lost remaining content or changed formatter")
			}
			record := b.state.Messages["partial-task"]
			if len(record.IDs) != 2 || record.IDs[0] != 77 || record.Hashes[0] != firstHash {
				t.Fatal("original part receipt changed")
			}
		})
	}
}
