package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
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
