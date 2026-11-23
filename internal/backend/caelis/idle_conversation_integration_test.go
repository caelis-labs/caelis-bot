package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/chatlog"
)

func TestNativeIdleResidentRenewal(t *testing.T) {
	if os.Getenv("CAELIS_BOT_TEST_BINARY") == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY for isolated external Host acceptance")
	}
	root := t.TempDir()
	history := filepath.Join(root, "ChatHistory", "conversation.jsonl")
	log := chatlog.Open(filepath.Join(root, "chat.sqlite"))
	defer log.Close()
	log.StartHistoryFile(history, nil)
	log.Observe([]api.Item{
		{ID: "original-input", RequestID: "case_idle_old", Kind: "user", Text: "Original fixture user message", Status: "accepted", SeenAt: time.Now().Add(-48 * time.Hour).UnixMicro()},
		{ID: "test-history", Kind: "assistant", Text: "CORE_NATIVE_HISTORY_FOUND", Status: "completed"},
	})
	ctx, s, resident, model := nativeDreamFixture(t, nil, func(r *bot.Runtime) {
		r.ConfigureIdleConversation(func(ctx context.Context, id string) (time.Time, bool, error) {
			return log.ConversationActivity(ctx, id, nil)
		})
	})
	waitAcceptance(t, ctx, func() bool {
		body, _ := os.ReadFile(history)
		return strings.Contains(string(body), "CORE_NATIVE_HISTORY_FOUND")
	})
	if err := os.WriteFile(filepath.Join(s.tools.NotebookDirectory, "MEMORY.md"), []byte("CORE_NATIVE_SHARED_MEMORY"), 0600); err != nil {
		t.Fatal(err)
	}
	submitAcceptance(t, ctx, s, "CASE_IDLE_OLD")
	original := s.ConversationState()
	wake := api.SystemMessage{ID: "idle-history-synthetic-wake", Source: api.SystemWorkerResult, Text: "CASE_IDLE_BACKGROUND"}
	if receipt, err := resident.SubmitSystem(ctx, wake); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("native system input did not reach the original resident", receipt, err)
	}
	waitTurn(t, ctx, s, original.Turn)
	if s.ConversationState().Session != original.Session {
		t.Fatal("background input replaced the resident")
	}
	args, _ := json.Marshal(map[string]string{"path": history})
	model.set("CASE_IDLE_NEW", modelStep{Name: "js_exec", Args: map[string]any{
		"code": "await tools.search({name:'Read'}); return await tools.call('Read'," + string(args) + ");",
	}})
	before := s.Snapshot().CurrentTurn
	if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "case_idle_new", Text: "CASE_IDLE_NEW"}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("native foreground input did not reach the replacement resident", receipt, err)
	}
	waitTurn(t, ctx, s, before)
	next := s.ConversationState()
	s.mu.Lock()
	oldPath := s.state.Operations[wake.ID].Path
	newPath := s.state.Operations["case_idle_new"].Path
	s.mu.Unlock()
	if next.Session == original.Session || newPath != "/sessions/"+idPath(next.Session)+"/prompt" ||
		oldPath != "/sessions/"+idPath(original.Session)+"/prompt" {
		t.Fatal("native receipts did not preserve distinct owners at the 48 hour boundary", oldPath, newPath)
	}
	calls := model.seen("CASE_IDLE_NEW")
	first, _ := json.Marshal(calls[0])
	result := fmt.Sprint(dtwLastToolOutput(calls))
	if !strings.Contains(string(first), "CORE_NATIVE_SHARED_MEMORY") {
		t.Fatal("replacement did not load MEMORY.md")
	}
	if strings.Contains(string(first), "CORE_NATIVE_HISTORY_FOUND") || strings.Contains(string(first), "CASE_IDLE_OLD") {
		t.Fatal("replacement received old chat in its initial model input")
	}
	if !strings.Contains(result, "CORE_NATIVE_HISTORY_FOUND") {
		t.Fatal("native Read did not retrieve the SQLite projection", result)
	}
	t.Log("Bot accepted a real foreground input after the SQLite user timestamp aged 48 hours despite an intervening native Worker notice; original receipt owners were preserved, and the new Session loaded MEMORY.md and retrieved the SQLite projection with Read")
}
