package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Opt in only with an explicit binary. This uses real native processes and
// persisted synthetic history, never an account, model turn, or user profile.
func TestNativeWorkerRetireCloseRestartHistory(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_NATIVE_LIFECYCLE_BINARY")
	if binary == "" {
		t.Skip("set CAELIS_BOT_NATIVE_LIFECYCLE_BINARY for isolated native acceptance")
	}
	version, err := exec.Command(binary, "--version").Output()
	if err != nil || strings.TrimSpace(string(version)) != "codex-cli 0.163.0" {
		t.Fatal("native acceptance requires reviewed Codex 0.163.0", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	ch := filepath.Join(home, ".codex")
	work := filepath.Join(root, "work")
	for _, dir := range []string{ch, work} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", ch)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEX_API_KEY", "")
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(503) }))
	defer provider.Close()
	config := fmt.Sprintf("thread_unload_delay_secs = 1\nmodel_provider = \"native_qa\"\nmodel = \"gpt-6-luna\"\n[model_providers.native_qa]\nname = \"No model calls\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\n", provider.URL+"/v1")
	if err := os.WriteFile(filepath.Join(ch, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	parent := "11111111-1111-4111-8111-111111111111"
	child := "22222222-2222-4222-8222-222222222222"
	taskID := "task-native-retained"
	seedNativeLegacyHistory(t, ch, work, parent, 1)
	seedNativeLegacyHistory(t, ch, work, child, 22)
	opts := SessionOptions{Binary: binary, Directory: work, StateFile: filepath.Join(root, "binding.json")}
	initial := binding{Version: 1, ThreadID: parent, Children: []string{child}, Tasks: map[string]*taskRecord{taskID: {Thread: child, Run: "original-turn-21", View: api.Task{ID: taskID, Status: "completed", Outcome: "accepted"}, Requests: map[string]taskReceipt{"original-receipt": {Outcome: "accepted", Phase: "accepted"}}}}}
	data, _ := json.Marshal(initial)
	if err := os.WriteFile(opts.StateFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	connect := func() *Session {
		s := NewSession(opts)
		t.Cleanup(func() { _ = s.Close(context.Background()) })
		if err := s.Connect(ctx); err != nil {
			t.Fatal(err)
		}
		if s.Snapshot().Connection != "ready" {
			t.Fatal("not ready", s.Snapshot().Connection)
		}
		return s
	}
	s := connect()
	read := func(s *Session) []api.TaskConversationMessage {
		t.Helper()
		if _, err := s.ReadWork(ctx, taskID); err != nil {
			t.Fatal(err)
		}
		m, err := s.ReadWorkConversation(ctx, taskID)
		if err != nil || len(m) != 44 {
			t.Fatal("original native history", len(m), err)
		}
		return m
	}
	first := read(s)
	// Load the exact child without sending a turn, then exercise Bot retirement.
	params := s.connectionParams()
	params["threadId"] = child
	var resumed threadExecutionResponse
	if err := callDecode(ctx, s.client, "thread/resume", params, &resumed); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.childSubscribed[child] = true
	s.binding.Tasks[taskID].Activity = "idle"
	c, epoch, seq := s.client, s.epoch, s.childRetireSeq[child]
	s.mu.Unlock()
	s.retireChild(c, epoch, child, seq)
	var unloaded error
	for i := 0; i < 100; i++ {
		_, unloaded = listBackgroundTerminals(ctx, c, child)
		if unloaded != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !nativeThreadError(unloaded, "thread not found: ", child) && !nativeThreadError(unloaded, "thread not loaded: ", child) {
		t.Fatal("native unload not reproduced", unloaded)
	}
	t.Log("real native retired-child error:", unloaded)
	if err := s.Close(ctx); err != nil {
		t.Fatal("ordinary close", err)
	}
	s.mu.Lock()
	unavailable, endpoint := s.binding.OwnerUnavailable, s.binding.OwnerEndpoint
	s.mu.Unlock()
	if unavailable || endpoint != "" {
		t.Fatal("settled owner retained as unavailable")
	}
	s = connect()
	if !reflect.DeepEqual(first, read(s)) {
		t.Fatal("restart changed original messages")
	}
	s.mu.Lock()
	sameParent := s.binding.ThreadID == parent
	s.mu.Unlock()
	if !sameParent {
		t.Fatal("ordinary restart replaced original parent")
	}
	t.Log("ordinary close/restart: same parent, 22 turns, 44 original messages")
	// A real process exit must not settle a synthetic unknown receipt. Recover
	// only this test owner through the existing explicit archival contract.
	s.mu.Lock()
	task := s.binding.Tasks[taskID]
	task.Pending = "original-unknown"
	task.Requests[task.Pending] = taskReceipt{Outcome: "unknown", Phase: "dispatching", PriorRun: task.Run}
	err = s.save()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close(ctx)
	blocked := NewSession(opts)
	t.Cleanup(func() { _ = blocked.Close(context.Background()) })
	if err := blocked.Connect(ctx); err == nil {
		t.Fatal("unknown receipt allowed ordinary replacement")
	}
	if handled, err := blocked.RecoverUnavailableOwner(ctx); !handled || err != nil {
		t.Fatal("isolated explicit recovery", handled, err)
	}
	if !reflect.DeepEqual(first, read(blocked)) {
		t.Fatal("archival changed original messages")
	}
	blocked.mu.Lock()
	task = blocked.binding.Tasks[taskID]
	protected := blocked.binding.LostOwnerTasks[taskID] && task.Thread == child && task.Run == "original-turn-21" && task.Pending == "original-unknown" && task.Requests[task.Pending].Outcome == "unknown"
	blocked.mu.Unlock()
	if !protected {
		t.Fatal("archival/history erased original uncertainty")
	}
	if _, err := blocked.SendWork(ctx, api.TaskMessage{ID: taskID, RequestID: "must-not-dispatch", Prompt: "must not execute"}); err == nil {
		t.Fatal("archived worker accepted new input")
	}
	if err := blocked.Close(ctx); err != nil {
		t.Fatal("new owner close", err)
	}
	if requests.Load() != 0 {
		t.Fatal("unexpected provider request", requests.Load())
	}
	if _, err := os.Stat(filepath.Join(ch, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("unexpected auth file", err)
	}
	t.Log("archived original history readable; unknown receipt preserved; provider requests=0; no auth file")
}

// Native 0.163.0 retains the legacy rollout reader. Synthetic records follow
// Codex's rollout test format; the actual protocol does all paging/projection.
func seedNativeLegacyHistory(t *testing.T, ch, work, id string, turns int) {
	t.Helper()
	dir := filepath.Join(ch, "sessions", "2026", "10", "20")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "rollout-2026-10-20T12-00-00-"+id+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	write := func(kind string, payload any) {
		t.Helper()
		if err := enc.Encode(map[string]any{"timestamp": "2026-10-20T12:00:00Z", "type": kind, "payload": payload}); err != nil {
			t.Fatal(err)
		}
	}
	write("session_meta", map[string]any{"id": id, "session_id": id, "timestamp": "2026-10-20T12:00:00Z", "cwd": work, "originator": "caelis_native_qa", "cli_version": "0.163.0", "source": "cli", "model_provider": "native_qa", "history_mode": "legacy"})
	for i := range turns {
		run := fmt.Sprintf("original-turn-%d", i)
		write("event_msg", map[string]any{"type": "task_started", "turn_id": run, "model_context_window": nil})
		write("event_msg", map[string]any{"type": "user_message", "message": fmt.Sprintf("Synthetic input %d", i), "text_elements": []any{}, "local_images": []any{}})
		write("event_msg", map[string]any{"type": "agent_message", "message": fmt.Sprintf("Synthetic answer %d", i)})
		write("event_msg", map[string]any{"type": "task_complete", "turn_id": run, "last_agent_message": fmt.Sprintf("Synthetic answer %d", i)})
	}
}
