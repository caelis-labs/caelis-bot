package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botskills"
	"github.com/caelis-labs/caelis-bot/internal/chatlog"
	notebookvault "github.com/caelis-labs/caelis-bot/internal/notebook"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

// Real installed App Server and stdio children, with deterministic local model
// replies and one synthetic upload. The host never calls StartTask directly.
func TestNativePrivateStdioWorkerRoundTrip(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_CODEX")
	if binary == "" {
		t.Skip("set CAELIS_BOT_TEST_CODEX for native protocol acceptance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, notebook := filepath.Join(root, "runtime"), filepath.Join(root, "Notebook")
	for _, dir := range []string{home, notebook} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	vault, err := notebookvault.OpenVault(notebook)
	if err != nil {
		t.Fatal(err)
	}
	defer vault.Close()
	if err := os.WriteFile(filepath.Join(notebook, "MEMORY.md"), []byte("CODEX_NATIVE_SHARED_MEMORY"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("CODEX_HOME", home)
	skill, err := botskills.Install(root)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	counts := map[string]int{}
	var taskID string
	var workerInput, workerFollowup, reportRead, isolated = false, false, false, true
	var reminderFired int
	var memoryFresh, historyFound bool
	history := filepath.Join(root, "ChatHistory", "conversation.jsonl")
	log := chatlog.Open(filepath.Join(root, "chat.sqlite"))
	defer log.Close()
	log.StartHistoryFile(history, nil)
	log.Observe([]api.Item{{ID: "history-test", Kind: "assistant", Text: "CODEX_NATIVE_HISTORY_FOUND", Status: "completed"}})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		body := string(raw)
		mu.Lock()
		role := "unexpected"
		switch {
		case strings.Contains(body, "risk_level") && strings.Contains(body, "user_authorization"):
			role = "guardian"
		case strings.Contains(body, skill):
			switch {
			case strings.Contains(body, "Bot system event: reminder"):
				role = "scheduled"
				reminderFired++
			case strings.Contains(body, "FIXTURE_REMINDER_REVOKE"):
				role = "revoke"
			case strings.Contains(body, "FIXTURE_REMINDER_REVOKABLE"):
				role = "save-revocable"
			case strings.Contains(body, "FIXTURE_REMINDER_REGISTER"):
				role = "save-reminder"
			case strings.Contains(body, "FIXTURE_USER_IDLE"):
				role = "idle-history"
				if counts[role] == 0 {
					memoryFresh = strings.Contains(body, "CODEX_NATIVE_SHARED_MEMORY") && !strings.Contains(body, "CODEX_NATIVE_HISTORY_FOUND") && !strings.Contains(body, "FIXTURE_USER_DELEGATE")
				} else {
					historyFound = historyFound || strings.Contains(body, "CODEX_NATIVE_HISTORY_FOUND")
				}
			case strings.Contains(body, "FIXTURE_USER_FOLLOWUP"):
				role = "followup"
			case strings.Contains(body, "Bot system event: worker_result"):
				role = "report"
			case strings.Contains(body, "FIXTURE_USER_DELEGATE"):
				role = "delegate"
			}
		case strings.Contains(body, "FIXTURE_WORKER_FOLLOWUP"):
			role = "worker-followup"
			workerFollowup = strings.Contains(body, "FIXTURE_FIRST_RESULT")
		case strings.Contains(body, "FIXTURE_WORKER_INPUT"):
			role = "worker"
			workerInput = strings.Contains(body, "selected.txt")
		}
		if role == "worker" || role == "worker-followup" {
			isolated = isolated && !strings.Contains(body, "caelis_tasks") && !strings.Contains(body, "bot-core") && !strings.Contains(body, history)
		}
		counts[role]++
		n := counts[role]
		task := taskID
		if role == "report" && n == 2 {
			reportRead = strings.Contains(body, "FIXTURE_FIRST_RESULT") && strings.Contains(body, "function_call_output")
		}
		mu.Unlock()
		id := fmt.Sprintf("%s-%d", role, n)
		reply := map[string]string{"worker": "FIXTURE_FIRST_RESULT", "worker-followup": "FIXTURE_SECOND_RESULT"}[role]
		if role == "guardian" {
			reply = `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"Only a synthetic file and a local fixture Worker are involved."}`
		}
		if reply == "" {
			reply = "FIXTURE_RESIDENT_ACK"
		}
		item := map[string]any{"id": id, "type": "message", "role": "assistant", "status": "completed", "phase": "final_answer",
			"content": []any{map[string]any{"type": "output_text", "text": reply, "annotations": []any{}}}}
		if n == 1 {
			name := ""
			namespace := "mcp__caelis_tasks"
			var args any
			switch role {
			case "delegate":
				name, args = "bot_delegate", map[string]any{"prompt": "FIXTURE_WORKER_INPUT", "attachments": []api.TaskFileReference{{ID: "selected-upload"}}}
			case "report":
				name, args = "bot_tasks", map[string]any{"request": map[string]any{"type": "read", "task": task}}
			case "followup":
				name, args = "bot_delegate", map[string]any{"prompt": "FIXTURE_WORKER_FOLLOWUP", "task": task}
			case "save-reminder", "save-revocable":
				name, namespace = "bot_schedule_update", "mcp__caelis_schedule"
				wait := 4 * time.Second
				if role == "save-revocable" {
					wait = 8 * time.Second
				}
				args = map[string]any{"request": map[string]any{
					"type": "save", "id": role, "label": role, "prompt": "Synthetic reminder " + role,
					"trigger": map[string]any{"type": "calendar", "timeZone": "UTC", "schedule": map[string]any{"type": "at", "at": time.Now().Add(wait).UTC().Format(time.RFC3339)}},
				}}
			case "revoke":
				name, namespace = "bot_schedule_update", "mcp__caelis_schedule"
				args = map[string]any{"request": map[string]any{"type": "remove", "automation": "calendar:save-revocable"}}
			case "idle-history":
				name, namespace = "exec_command", ""
				args = map[string]any{"cmd": fmt.Sprintf("rg -n 'NATIVE_HISTORY' %q", history), "workdir": notebook}
			}
			if name != "" {
				arguments, _ := json.Marshal(args)
				call := map[string]any{"id": id, "type": "function_call", "name": name, "call_id": id, "arguments": string(arguments)}
				if namespace != "" {
					call["namespace"] = namespace
				}
				item = call
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(event any) { b, _ := json.Marshal(event); fmt.Fprintf(w, "data: %s\n\n", b) }
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	defer provider.Close()
	config := fmt.Sprintf("model = \"fixture\"\nmodel_provider = \"fixture\"\n[model_providers.fixture]\nname = \"Local fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\nsupports_websockets = false\n", provider.URL)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	resident, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := bot.Serve(resident)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	connection := bridge.Config(executable)
	connection.Args = []string{"-test.run=^TestDreamMCPHelper$"}
	connection.Env["CAELIS_BOT_MCP_HELPER"], connection.Env["FIXTURE_AUTH"] = "1", "synthetic-only"
	connection.Instructions, connection.SkillInstructions = botskills.Bootstrap(), botskills.Instructions(skill)
	connection.Instructions += "\nRead-only fixture conversation: " + history
	connection.PrepareContext = vault.PrepareContext
	connection.NotebookDirectory = notebook
	work := filepath.Join(root, "Tasks")
	opts := SessionOptions{Binary: binary, Directory: notebook, WorkRoot: work, StateFile: filepath.Join(root, "binding.json")}
	s := NewSession(opts)
	defer s.Close(context.Background())
	if err := s.ConfigureBotTools(connection); err != nil {
		t.Fatal(err)
	}
	manager, err := tasks.Open(filepath.Join(root, "tasks.json"), work, "codex", s, resident, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	const selected = "FIXTURE_SELECTED_ATTACHMENT_BYTES"
	manager.ConfigureInputResolver(func(_ context.Context, id string) (api.WorkFileContent, error) {
		if id != "selected-upload" {
			return api.WorkFileContent{}, errors.New("unselected attachment")
		}
		return api.WorkFileContent{Name: "selected.txt", MediaType: "text/plain", Data: []byte(selected)}, nil
	})
	if err := resident.ConfigureTasks(manager, nil); err != nil {
		t.Fatal(err)
	}
	resident.Start(s)
	defer resident.Close()
	if err := s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	submit := func(id, prompt string) {
		t.Helper()
		before := s.ConversationState().TurnKey
		receipt, err := resident.SubmitUser(ctx, api.Submission{ID: id, Text: prompt}, nil)
		if err != nil || receipt.Outcome != "accepted" {
			t.Fatal(receipt.Outcome, err)
		}
		awaitState(t, s, func(v api.Snapshot) bool {
			return s.ConversationState().TurnKey != before && v.CanSend && v.Phase == "completed"
		})
	}
	waitWorker := func() api.Task {
		t.Helper()
		for {
			list := manager.ListTasks()
			if len(list) == 1 && list[0].Status == "completed" {
				return list[0]
			}
			select {
			case <-ctx.Done():
				t.Fatal("original Worker completion unavailable", len(list))
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	submit("mcp-start", "FIXTURE_USER_DELEGATE")
	first := waitWorker()
	paths, err := filepath.Glob(filepath.Join(notebook, ".attachments", "input-*", "0-selected.txt"))
	if err != nil || len(paths) != 1 {
		t.Fatal("native attachment staging missing or duplicated", len(paths), err)
	}
	if data, err := os.ReadFile(paths[0]); err != nil || string(data) != selected {
		t.Fatal("selected attachment bytes were not retained", err)
	}
	s.mu.Lock()
	originalThread := s.binding.Tasks[first.ID].Thread
	s.mu.Unlock()
	mu.Lock()
	taskID = first.ID
	mu.Unlock()
	before := s.ConversationState().TurnKey
	if err := manager.DeliverTaskReport(ctx); err != nil {
		t.Fatal(err)
	}
	awaitState(t, s, func(v api.Snapshot) bool {
		return s.ConversationState().TurnKey != before && v.CanSend && v.Phase == "completed"
	})
	submit("mcp-followup", "FIXTURE_USER_FOLLOWUP")
	second := waitWorker()
	s.mu.Lock()
	sameThread := s.binding.Tasks[second.ID].Thread == originalThread
	s.mu.Unlock()
	if first.ID != second.ID || !sameThread {
		t.Fatal("follow-up replaced the original Worker")
	}
	head := s.ConversationState()
	if err := s.RenewIdleConversation(ctx, "native-idle", head); err != nil {
		t.Fatal(err)
	}
	if s.ConversationState().Session == head.Session {
		t.Fatal("original resident remained bound")
	}
	submit("find-idle-history", "FIXTURE_USER_IDLE")
	runReminder := func(key string) {
		t.Helper()
		previous := s.ConversationState().TurnKey
		msg := api.SystemMessage{ID: strings.ToLower(key), Text: key, Source: api.SystemWorkerResult}
		if out, err := resident.SubmitSystem(ctx, msg); err != nil || out.Outcome != "accepted" {
			t.Fatal("native reminder input refused", out, err)
		}
		awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend && s.ConversationState().TurnKey != previous })
	}
	runReminder("FIXTURE_REMINDER_REGISTER")
	runReminder("FIXTURE_REMINDER_REVOKABLE")
	var deadline time.Time
	for _, arrangement := range resident.State().Schedules {
		if arrangement.ID == "save-revocable" {
			deadline = arrangement.Next
		}
	}
	if deadline.IsZero() {
		t.Fatal("native scheduled tool did not register arrangement")
	}
	runReminder("FIXTURE_REMINDER_REVOKE")
	for _, arrangement := range resident.State().Schedules {
		if arrangement.ID == "save-revocable" {
			t.Fatal("native scheduled tool did not revoke arrangement")
		}
	}
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(time.Until(deadline.Add(1200 * time.Millisecond))):
	}
	mu.Lock()
	fired := reminderFired
	mu.Unlock()
	if fired != 1 {
		t.Fatal("expected one native reminder activation after registration and revocation", fired)
	}
	originalResident := s.ConversationState().Session
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	restored := NewSession(opts)
	defer restored.Close(context.Background())
	if err := restored.ConfigureBotTools(connection); err != nil {
		t.Fatal(err)
	}
	if err := restored.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if restored.ConversationState().Session != originalResident {
		t.Fatal("restart replaced the original resident")
	}
	messages, err := restored.ReadWorkConversation(ctx, second.ID)
	if err != nil || len(messages) != 4 || messages[1].Text != "FIXTURE_FIRST_RESULT" || messages[3].Text != "FIXTURE_SECOND_RESULT" {
		t.Fatal("the original native Worker did not retain both results", len(messages), err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !workerInput || !workerFollowup || !reportRead || !isolated || !memoryFresh || !historyFound || counts["worker"] != 1 || counts["worker-followup"] != 1 || counts["guardian"] < 2 {
		t.Fatal("native round trip was incomplete", counts, workerInput, workerFollowup, reportRead, isolated)
	}
	t.Log("resident stdio MCP delegated twice through Guardian, read the completion via its direct tool, and continued the same Worker; restart retained both results")
	t.Log("native MCP registered reminders from Worker material, triggered one and suppressed the revoked one past its due time")
	t.Log("native idle thread loaded shared memory and queried SQLite-projected history with rg; original Worker history survived")
}
