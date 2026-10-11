package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
	tg "github.com/mymmrac/telego"
)

func TestTelegramAsyncOptionsUseButtonsAndExactSharedAnswer(t *testing.T) {
	control, err := textchannel.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	item := api.Item{ID: "agent-item", TurnKey: "turn", Kind: "assistant", AsyncCallID: "native-call", AsyncQuestions: []api.AsyncQuestion{{Title: "请选择", Options: []string{"甲", "乙", "丙"}}}}
	card, err := control.AsyncCard(item, "owner")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := api.Snapshot{Connection: "ready", RuntimeOwner: "owner", Items: []api.Item{card}}
	var modelInput string
	calls := 0
	control.SetAsyncAnswerer(func(_ context.Context, _ textchannel.Inbound, input string) (api.Receipt, error) {
		calls++
		modelInput = input
		return api.Receipt{Outcome: "accepted"}, nil
	})
	b, client := testBridge(t, Host{Snapshot: func() api.Snapshot { return snapshot }, TextControl: control})
	paired(b)
	b.mirror(t.Context(), client, snapshot)
	record := b.state.Messages[itemKey(card)]
	if client.sends != 1 || record.Keyboard == "" || len(record.IDs) != 1 || len(client.keyboards) != 1 || client.keyboards[0] == nil || len(client.keyboards[0].InlineKeyboard) != 3 {
		t.Fatalf("button card missing: %#v %#v", record, client.keyboards)
	}
	if client.texts[0] != "[Q1] 请选择" || strings.Contains(client.texts[0], "/answer") || strings.Contains(client.texts[0], "[1] 甲") {
		t.Fatalf("duplicated text choices: %q", client.texts[0])
	}
	button := client.keyboards[0].InlineKeyboard[1][0]
	if button.Text != "乙" {
		t.Fatal(button)
	}
	// The Runtime may compact this completed item from its current snapshot;
	// the confirmed Telegram message and shared question record still bind it.
	snapshot.Items = nil
	query := &tg.CallbackQuery{ID: "first", From: tg.User{ID: 20}, Message: &tg.Message{MessageID: record.IDs[0], Chat: tg.Chat{ID: 10}}, Data: button.CallbackData}
	if !b.callback(t.Context(), client, query) {
		t.Fatal("callback not handled")
	}
	b.recoveryWait.Wait()
	if calls != 1 || !strings.Contains(modelInput, `"answer":"乙"`) || !strings.Contains(modelInput, `"questionItemId":"[\"request_user_input_async\",\"native-call\",0]"`) {
		t.Fatalf("wrong native question answer: calls=%d input=%q", calls, modelInput)
	}
	if got := b.state.Messages[itemKey(card)]; got.Keyboard != "" || client.edits != 1 || !strings.Contains(client.texts[len(client.texts)-1], "回答已提交") {
		t.Fatalf("question button remained active: %#v texts=%#v", got, client.texts)
	}
	query.ID = "stale"
	b.callback(t.Context(), client, query)
	if calls != 1 {
		t.Fatal("stale button replayed answer")
	}
}

func TestTelegramFreeAsyncQuestionKeepsCopyableText(t *testing.T) {
	control, _ := textchannel.Open(t.TempDir(), nil)
	item := api.Item{ID: "free-item", TurnKey: "turn", Kind: "assistant", AsyncCallID: "call", AsyncQuestions: []api.AsyncQuestion{{Title: "请说明"}}}
	card, _ := control.AsyncCard(item, "owner")
	snapshot := api.Snapshot{Connection: "ready", RuntimeOwner: "owner", Items: []api.Item{card}}
	b, client := testBridge(t, Host{Snapshot: func() api.Snapshot { return snapshot }, TextControl: control})
	paired(b)
	b.mirror(t.Context(), client, snapshot)
	if client.sends != 1 || !strings.Contains(client.texts[0], "/answer Q1") || client.keyboards[0] != nil {
		t.Fatalf("free text fallback changed: %#v %#v", client.texts, client.keyboards)
	}
}

func TestTelegramMixedAsyncQuestionsKeepFreeCommandAndFixedButtons(t *testing.T) {
	control, _ := textchannel.Open(t.TempDir(), nil)
	item := api.Item{ID: "mixed", TurnKey: "turn", Kind: "assistant", AsyncCallID: "call", AsyncQuestions: []api.AsyncQuestion{{Title: "选择", Options: []string{"甲", "乙"}}, {Title: "说明"}}}
	card, _ := control.AsyncCard(item, "owner")
	snapshot := api.Snapshot{Connection: "ready", RuntimeOwner: "owner", Items: []api.Item{card}}
	b, client := testBridge(t, Host{Snapshot: func() api.Snapshot { return snapshot }, TextControl: control})
	paired(b)
	b.mirror(t.Context(), client, snapshot)
	if client.sends != 1 || client.keyboards[0] == nil || len(client.keyboards[0].InlineKeyboard) != 2 {
		t.Fatalf("fixed question lost buttons: %#v", client.keyboards)
	}
	if !strings.Contains(client.texts[0], "/answer Q2 后接完整回答") || strings.Contains(client.texts[0], "[1] 甲") || client.keyboards[0].InlineKeyboard[1][0].Text != "[Q1] 乙" {
		t.Fatalf("mixed questions duplicated fixed choices or lost free instruction: %q %#v", client.texts[0], client.keyboards[0])
	}
}

func TestTelegramApprovalButtonsOmitDuplicateChoiceList(t *testing.T) {
	control, _ := textchannel.Open(t.TempDir(), nil)
	a := api.Approval{ID: "native", Owner: "owner", Status: "pending", Title: "cua_repl", Target: "Obsidian", Description: "Allow Computer Use to use Obsidian?", Choices: []api.Choice{{ID: "once", Label: "允许这一次", Scope: "once"}, {ID: "deny", Label: "拒绝", Scope: "deny"}}}
	snapshot := api.Snapshot{Connection: "ready", RuntimeOwner: "owner", Approvals: []api.Approval{a}}
	b, client := testBridge(t, Host{Snapshot: func() api.Snapshot { return snapshot }, TextControl: control})
	paired(b)
	b.mirror(t.Context(), client, snapshot)
	if client.sends != 1 || client.keyboards[0] == nil || len(client.keyboards[0].InlineKeyboard) != 2 {
		t.Fatalf("approval buttons missing: %#v", client.keyboards)
	}
	if !strings.Contains(client.texts[0], "目标：Obsidian") || strings.Contains(client.texts[0], "[1]") || strings.Contains(client.texts[0], "/approve") {
		t.Fatalf("approval body duplicated choices or lost context: %q", client.texts[0])
	}
	if _, short, err := control.Card(a); err != nil || short != "A1" {
		t.Fatalf("shared command lost: %q %v", short, err)
	}
}
