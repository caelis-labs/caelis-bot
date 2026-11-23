package caelis

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/chatlog"
)

func TestNativeIdleResidentRenewal(t *testing.T) {
	ctx, s, _, model := nativeDreamFixture(t, nil)
	root := t.TempDir()
	history := filepath.Join(root, "ChatHistory", "conversation.jsonl")
	log := chatlog.Open(filepath.Join(root, "chat.sqlite"))
	defer log.Close()
	log.StartHistoryFile(history, nil)
	log.Observe([]api.Item{{ID: "test-history", Kind: "assistant", Text: "CORE_NATIVE_HISTORY_FOUND", Status: "completed"}})
	waitAcceptance(t, ctx, func() bool {
		body, _ := os.ReadFile(history)
		return strings.Contains(string(body), "CORE_NATIVE_HISTORY_FOUND")
	})
	if err := os.WriteFile(filepath.Join(s.tools.NotebookDirectory, "MEMORY.md"), []byte("CORE_NATIVE_SHARED_MEMORY"), 0600); err != nil {
		t.Fatal(err)
	}
	submitAcceptance(t, ctx, s, "CASE_IDLE_OLD")
	original := s.ConversationState()
	if err := s.RenewIdleConversation(ctx, "native-test-idle", original); err != nil {
		t.Fatal(err)
	}
	if s.ConversationState().Session == original.Session {
		t.Fatal("native binding did not change")
	}
	args, _ := json.Marshal(map[string]string{"path": history})
	model.set("CASE_IDLE_NEW", modelStep{Name: "js_exec", Args: map[string]any{
		"code": "await tools.search({name:'Read'}); return await tools.call('Read'," + string(args) + ");",
	}})
	submitAcceptance(t, ctx, s, "CASE_IDLE_NEW")
	calls := model.seen("CASE_IDLE_NEW")
	first, _ := json.Marshal(calls[0])
	result := fmt.Sprint(dtwLastToolOutput(calls))
	if !strings.Contains(string(first), "CORE_NATIVE_SHARED_MEMORY") ||
		strings.Contains(string(first), "CORE_NATIVE_HISTORY_FOUND") || strings.Contains(string(first), "CASE_IDLE_OLD") ||
		!strings.Contains(result, "CORE_NATIVE_HISTORY_FOUND") {
		t.Fatal("Core should load MEMORY.md and retrieve history through native Read")
	}
	t.Log("native empty Session loaded MEMORY.md, then Read retrieved an SQLite-projected row outside Notebook")
}
