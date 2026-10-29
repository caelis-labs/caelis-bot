package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

type conversationOwner struct {
	api.WorkRuntime
	api.SystemDelivery
	historyError error
	ids          []string
}

func (*conversationOwner) WorkStates() []api.WorkState {
	return []api.WorkState{{Task: api.Task{ID: "original-task", Status: "completed", Outcome: "accepted"}}}
}
func (f *conversationOwner) ReadWork(_ context.Context, id string) (api.Task, error) {
	f.ids = append(f.ids, id)
	return api.Task{ID: id, Status: "completed", Outcome: "accepted"}, nil
}
func (f *conversationOwner) ReadWorkConversation(_ context.Context, id string) ([]api.TaskConversationMessage, error) {
	f.ids = append(f.ids, id)
	if f.historyError != nil {
		return nil, f.historyError
	}
	return []api.TaskConversationMessage{{ID: "original-user", Role: "user", Text: "fixture question"}, {ID: "original-final", Role: "assistant", Text: "fixture final answer"}}, nil
}

func TestBotAndWebReadSameOriginalTaskConversation(t *testing.T) {
	root := t.TempDir()
	f := &conversationOwner{}
	m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", f, f, func() api.Snapshot { return api.Snapshot{} })
	if err != nil {
		t.Fatal(err)
	}
	a := &Application{tasks: m}
	r, err := bot.New(filepath.Join(root, "bot.json"), nil)
	if err != nil || r.ConfigureTasks(m, nil) != nil {
		t.Fatal("task port unavailable")
	}
	web, err := a.SurfaceTaskConversation(t.Context(), "original-task")
	if err != nil || web.Status != "completed" || len(web.Messages) != 2 {
		t.Fatal("Web task conversation unavailable", err)
	}
	call := func(query map[string]any) api.ToolResult {
		t.Helper()
		query["type"], query["task"] = "read", "original-task"
		raw, _ := json.Marshal(map[string]any{"request": query})
		return r.CallTool(t.Context(), "bot_tasks", raw)
	}
	page := call(map[string]any{"offset": 1, "limit": 1})
	if page.IsError || !reflect.DeepEqual(page.StructuredContent["data"].(map[string]any)["messages"], web.Messages[1:]) {
		t.Fatal("Bot page lost the Web final message")
	}
	detail := call(map[string]any{"messageId": web.Messages[1].ID})
	if detail.IsError || !reflect.DeepEqual(detail.StructuredContent["data"].(map[string]any)["message"], web.Messages[1]) {
		t.Fatal("Bot detail changed original message identity")
	}
	f.historyError = errors.New("fixture native history unavailable")
	if failed, err := a.SurfaceTaskConversation(t.Context(), "original-task"); err == nil || failed.Messages != nil {
		t.Fatal("Web converted a native read failure to empty success")
	}
	failed := call(map[string]any{})
	if !failed.IsError || failed.StructuredContent["error"].(map[string]string)["code"] != "native_history_unreadable" {
		t.Fatal("Bot hid a real native read failure")
	}
	for _, id := range f.ids {
		if id != "original-task" {
			t.Fatal("read rebound the original task")
		}
	}
}
