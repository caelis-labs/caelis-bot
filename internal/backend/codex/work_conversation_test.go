package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
)

func TestWorkerConversationReadsNativeFullPagesAndOnlyCompletedMessages(t *testing.T) {
	s, fixture, _, manager := taskPair(t)
	sendSynthetic(t, s, "native-history-parent")
	task := newTask(t, manager, "native-message-history")
	s.mu.Lock()
	threadID := s.binding.Tasks[task.ID].Thread
	s.mu.Unlock()
	turns := make([]nativeTurn, 0, 22)
	for i := range 21 {
		turns = append(turns, nativeTurn{ID: fmt.Sprintf("turn-%02d", i), Status: "completed", Items: []nativeItem{
			{ID: "user", Type: "userMessage", Content: []nativeInput{{Type: "text", Text: fmt.Sprintf("input %d", i)}}},
			{ID: "tool", Type: "commandExecution", Output: "private tool output"},
			{ID: "reply", Type: "agentMessage", Text: fmt.Sprintf("answer %d", i)},
		}})
	}
	turns = append(turns, nativeTurn{ID: "active-turn", Status: "inProgress", Items: []nativeItem{
		{ID: "active-user", Type: "userMessage", Content: []nativeInput{{Type: "text", Text: "follow up"}}},
		{ID: "complete-process", Type: "agentMessage", Status: "completed", Text: "process complete"},
		{ID: "partial", Type: "agentMessage", Status: "inProgress", Text: "partial secret"},
	}})
	fixture.mu.Lock()
	worker := fixture.workers[threadID]
	worker.Turns = turns
	fixture.workers[threadID] = worker
	fixture.mu.Unlock()
	fixture.emit(wireMessage{Method: "item/completed", Params: raw(map[string]any{"threadId": threadID, "turnId": "active-turn", "item": nativeItem{ID: "complete-process", Type: "agentMessage", Text: "process complete"}})})
	awaitState(t, s, func(api.Snapshot) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.binding.Tasks[task.ID].CompletedMessages[opaque("active-turn", "complete-process")]
	})
	data, err := os.ReadFile(s.opts.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	var restored binding
	if json.Unmarshal(data, &restored) != nil || !restored.Tasks[task.ID].CompletedMessages[opaque("active-turn", "complete-process")] {
		t.Fatal("native completed-item identity was not durable")
	}
	conversation, err := manager.ReadTaskConversation(testContext(t), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Status != "working" || len(conversation.Messages) != 44 {
		t.Fatal("full FIFO history was not projected", conversation.Status, len(conversation.Messages))
	}
	if first := conversation.Messages[0]; first.Role != "user" || first.Text != "input 0" || first.ID == "" {
		t.Fatal("first original message missing", first)
	}
	if last := conversation.Messages[len(conversation.Messages)-1]; last.Role != "assistant" || last.Text != "process complete" {
		t.Fatal("completed process message missing", last)
	}
	if previous := conversation.Messages[len(conversation.Messages)-2]; previous.Role != "user" || previous.Text != "follow up" {
		t.Fatal("latest user message missing", previous)
	}
	for _, message := range conversation.Messages {
		if message.Text == "partial secret" || message.Text == "private tool output" {
			t.Fatal("unfinished or tool output leaked", message)
		}
	}
	if _, err := s.ReadWorkConversation(testContext(t), "foreign"); err == nil {
		t.Fatal("foreign task history was readable")
	}
}

func TestWorkerConversationRecoversHistoricalProgressWithoutLocalItemReceipts(t *testing.T) {
	s, fixture, _, manager := taskPair(t)
	sendSynthetic(t, s, "history-parent-without-markers")
	task := newTask(t, manager, "history-without-markers")
	s.mu.Lock()
	thread := s.binding.Tasks[task.ID].Thread
	s.binding.Tasks[task.ID].CompletedMessages = nil // pre-upgrade or missed event
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	fixture.mu.Lock()
	worker := fixture.workers[thread]
	worker.Turns = []nativeTurn{
		{ID: "old-complete", Status: "completed", Items: []nativeItem{
			{ID: "user", Type: "userMessage", Content: []nativeInput{{Type: "text", Text: "start"}}},
			{ID: "progress", Type: "agentMessage", Phase: "commentary", Text: "finished progress"},
			{ID: "tool", Type: "commandExecution", Output: "private"},
			{ID: "final", Type: "agentMessage", Phase: "final_answer", Text: "finished result"},
		}},
		{ID: "steered", Status: "completed", Items: []nativeItem{
			{ID: "user", Type: "userMessage", Content: []nativeInput{{Type: "text", Text: "more"}}},
			{ID: "partial", Type: "agentMessage", Text: "interrupted prefix"},
			{ID: "steer-user", Type: "userMessage", Content: []nativeInput{{Type: "text", Text: "steer"}}},
			{ID: "final", Type: "agentMessage", Text: "complete after steer"},
		}},
		{ID: "working", Status: "inProgress", Items: []nativeItem{
			{ID: "user", Type: "userMessage", Content: []nativeInput{{Type: "text", Text: "continue"}}},
			{ID: "done", Type: "agentMessage", Text: "finished while working"},
			{ID: "tool", Type: "commandExecution"},
			{ID: "stream", Type: "agentMessage", Text: "unfinished stream"},
		}},
	}
	fixture.workers[thread] = worker
	fixture.mu.Unlock()
	assertHistory := func() []api.TaskConversationMessage {
		t.Helper()
		messages, err := s.ReadWorkConversation(testContext(t), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"start", "finished progress", "finished result", "more", "steer", "complete after steer", "continue", "finished while working"}
		if len(messages) != len(want) {
			t.Fatalf("native history lost completed messages: %+v", messages)
		}
		for i, message := range messages {
			if message.Text != want[i] || message.ID == "" {
				t.Fatalf("message %d: %+v", i, message)
			}
		}
		return messages
	}
	before := assertHistory()
	resident, err := bot.NewForRuntime(filepath.Join(t.TempDir(), "bot.json"), "codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resident.ConfigureTasks(manager, nil); err != nil {
		t.Fatal(err)
	}
	listArgs, _ := json.Marshal(map[string]any{"request": map[string]any{"type": "read", "task": task.ID, "limit": 20}})
	list := resident.CallTool(t.Context(), "bot_tasks", listArgs)
	if list.IsError {
		t.Fatal("Bot list could not read original native history", list)
	}
	listed := list.StructuredContent["data"].(map[string]any)["messages"].([]api.TaskConversationMessage)
	if len(listed) != len(before) || listed[1].ID != before[1].ID || listed[1].Text != "finished progress" {
		t.Fatal("Bot list dropped pre-upgrade progress", listed)
	}
	detailArgs, _ := json.Marshal(map[string]any{"request": map[string]any{"type": "read", "task": task.ID, "messageId": before[1].ID}})
	detail := resident.CallTool(t.Context(), "bot_tasks", detailArgs)
	if detail.IsError || detail.StructuredContent["data"].(map[string]any)["message"].(api.TaskConversationMessage).Text != "finished progress" {
		t.Fatal("Bot messageId detail dropped pre-upgrade progress", detail)
	}
	data, err := os.ReadFile(s.opts.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	var restored binding
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.binding = restored
	s.mu.Unlock()
	after := assertHistory()
	for i := range before {
		if before[i].ID != after[i].ID {
			t.Fatal("restart changed native message identity", i)
		}
	}
}
