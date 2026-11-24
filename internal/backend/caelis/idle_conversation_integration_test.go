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

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
)

func TestNativeIdleResidentRenewal(t *testing.T) {
	if os.Getenv("CAELIS_BOT_TEST_BINARY") == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY for isolated external Host acceptance")
	}
	for _, channel := range []string{"desktop", "telegram"} {
		t.Run(channel, func(t *testing.T) { testNativeIdleResidentRenewal(t, channel) })
	}
}

func testNativeIdleResidentRenewal(t *testing.T, channel string) {
	t.Helper()
	root := t.TempDir()
	history := filepath.Join(root, "ChatHistory", "conversation.jsonl")
	var service *backend.Service
	ctx, s, resident, model := nativeDreamFixture(t, nil, func(r *bot.Runtime) {
		r.ConfigureIdleConversation(func(ctx context.Context, id string) (time.Time, bool, error) {
			return service.BotConversationActivity(ctx, id, nil)
		})
	})
	service = backend.NewService(s, func([]string) ([]api.InputFile, error) { return nil, nil }, nil, nil, nil)
	service.ConfigureChat(filepath.Join(root, "chat.sqlite"))
	defer service.Shutdown()
	service.SetUserSubmitter(resident.SubmitUser)
	service.StartBotHistoryFile(history, nil)
	requestID := channel + ":synthetic:1"
	service.ObserveChat(api.Snapshot{Items: []api.Item{
		{ID: "original-input", RequestID: "case_idle_old", Kind: "user", Text: "Original fixture user message", Status: "accepted", SeenAt: time.Now().Add(-49 * time.Hour).UnixMicro()},
		{ID: "test-history", Kind: "assistant", Text: "CORE_NATIVE_HISTORY_FOUND", Status: "completed"},
	}})
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
	// The App ingress regression exercises the handler that writes this receipt.
	service.ObserveChat(api.Snapshot{Items: []api.Item{
		{ID: "control-input:desktop-status", RequestID: "desktop-status", Kind: "user", Text: "/status", Status: "accepted"},
	}})
	submit := service.Submit
	if channel == "telegram" {
		submit = func(ctx context.Context, in api.Submission) (api.Receipt, error) {
			service.ObserveChat(api.Snapshot{Items: []api.Item{
				{ID: "channel-input:" + in.ID, RequestID: in.ID, Kind: "user", Text: in.Text, Status: "received"},
			}})
			return backend.SubmitRemote(ctx, service, in, nil)
		}
	}
	args, _ := json.Marshal(map[string]string{"path": history})
	model.set("CASE_IDLE_NEW", modelStep{Name: "js_exec", Args: map[string]any{
		"code": "await tools.search({name:'Read'}); return await tools.call('Read'," + string(args) + ");",
	}})
	before := s.Snapshot().CurrentTurn
	input := api.Submission{ID: requestID, Text: "CASE_IDLE_NEW"}
	if receipt, err := submit(ctx, input); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("native foreground input did not reach the replacement resident", receipt, err)
	}
	waitTurn(t, ctx, s, before)
	next := s.ConversationState()
	s.mu.Lock()
	oldPath := s.state.Operations[wake.ID].Path
	newPath := s.state.Operations[requestID].Path
	s.mu.Unlock()
	if next.Session == original.Session || newPath != "/sessions/"+idPath(next.Session)+"/prompt" ||
		oldPath != "/sessions/"+idPath(original.Session)+"/prompt" {
		t.Fatal("native receipts did not preserve distinct owners after 49 idle hours", oldPath, newPath)
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
	if receipt, err := submit(ctx, input); err != nil || receipt.Outcome != "accepted" ||
		s.ConversationState().Session != next.Session || len(model.seen("CASE_IDLE_NEW")) != len(calls) {
		t.Fatal("retry did not retain the original native result", receipt, err)
	}
	t.Logf("%s ingress after 49 idle hours and a desktop control receipt used the next native Session once; the Worker notice stayed with the old Session, and the new Session retrieved projected history through Read", channel)
}
