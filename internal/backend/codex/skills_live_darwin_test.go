package codex

import (
	"context"
	"encoding/json"
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
	"github.com/caelis-labs/caelis-bot/internal/botskills"
	"github.com/caelis-labs/caelis-bot/internal/care"
)

// Real App Server and native file tool, local deterministic provider. No account
// or global skill installation; captured requests contain only synthetic data.
func TestNativeProgressiveSkill(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_CODEX")
	if binary == "" {
		t.Skip("set CAELIS_BOT_TEST_CODEX to the installed CLI")
	}
	dir := t.TempDir()
	home := filepath.Join(dir, "runtime")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	skillPath, err := botskills.Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []string
	workerRequested := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		mu.Lock()
		requests = append(requests, string(body))
		n := len(requests)
		mu.Unlock()
		if n == 4 {
			defer close(workerRequested)
		}
		id := fmt.Sprintf("skill-%d", n)
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); w.(http.Flusher).Flush() }
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		var item map[string]any
		if n <= 2 {
			p := skillPath
			if n == 2 {
				p = filepath.Join(filepath.Dir(skillPath), "references", "tasks.md")
			}
			args, _ := json.Marshal(map[string]any{"cmd": "cat '" + strings.ReplaceAll(p, "'", "'\"'\"'") + "'", "max_output_tokens": 4000})
			item = map[string]any{"id": id, "type": "function_call", "name": "exec_command", "call_id": id, "arguments": string(args)}
		} else {
			item = map[string]any{"id": id, "type": "message", "role": "assistant", "status": "completed", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": "skill-read-complete", "annotations": []any{}}}}
		}
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	defer provider.Close()
	config := fmt.Sprintf("model = \"skill-fixture\"\nmodel_provider = \"skill-fixture\"\n[model_providers.skill-fixture]\nname = \"Local fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\nsupports_websockets = false\n", provider.URL)
	if err = os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	s := NewSession(SessionOptions{Binary: binary, Directory: filepath.Join(dir, "Notebook"), StateFile: filepath.Join(dir, "binding.json"), BotTools: &api.ToolConnection{Command: "/usr/bin/false", Args: []string{"--fixture"}, Env: map[string]string{}, Instructions: botskills.Instructions(skillPath)}})
	defer func() {
		c, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = s.Close(c)
	}()
	if err = s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := s.Submit(ctx, api.Submission{ID: "progressive-skill-input", Text: "Read the applicable guide and its task reference."}, nil)
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	var revision uint64
	for {
		v, err := s.WaitSnapshot(ctx, revision)
		if err != nil {
			t.Fatal(err)
		}
		revision = v.Revision
		if v.Phase == "completed" {
			break
		}
		if v.Phase == "failed" || len(v.Approvals) > 0 {
			t.Fatal("native skill read failed", v.Phase, v.Message)
		}
	}
	mu.Lock()
	initial := append([]string(nil), requests...)
	mu.Unlock()
	if len(initial) != 3 {
		t.Fatalf("expected two native file reads: %d model requests", len(initial))
	}
	if !strings.Contains(initial[0], "You are Caelis Bot, a persistent personal assistant.") || strings.Contains(initial[0], "# Restore your context") {
		t.Fatal("metadata absent or eager body injection")
	}
	if !strings.Contains(initial[1], "# Restore your context") || strings.Contains(initial[1], "# Arrange and continue independent work") || !strings.Contains(initial[2], "# Arrange and continue independent work") {
		t.Fatal("native file tools did not progressively load skill")
	}
	correlated := false
	for _, item := range s.Snapshot().Items {
		if item.Kind == "user" && item.RequestID == "progressive-skill-input" {
			correlated = true
		}
	}
	if !correlated {
		t.Fatal("native user input did not preserve submission identity")
	}
	workerDir := filepath.Join(dir, "Tasks", "isolated")
	if err = os.MkdirAll(workerDir, 0700); err != nil {
		t.Fatal(err)
	}
	var worker threadExecutionResponse
	if err = callDecode(ctx, s.client, "thread/start", s.workerParams(workerDir, "Complete the assigned task.", &taskRecord{}), &worker); err != nil {
		t.Fatal(err)
	}
	var started struct {
		Turn nativeTurn `json:"turn"`
	}
	if err = callDecode(ctx, s.client, "turn/start", map[string]any{"threadId": worker.Thread.ID, "input": []nativeInput{{Type: "text", Text: "Worker isolation check", TextElements: []any{}}}}, &started); err != nil {
		t.Fatal(err)
	}
	select {
	case <-workerRequested:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	mu.Lock()
	workerRequest := requests[3]
	mu.Unlock()
	if strings.Contains(workerRequest, skillPath) || strings.Contains(workerRequest, "You are Caelis Bot, a persistent personal assistant.") {
		t.Fatal("resident skill leaked into native worker")
	}
	careEngine, err := care.Open(filepath.Join(dir, "care.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = careEngine.Save(ctx, care.Rule{ID: "native-care", Label: "Care fixture", On: "clock.minute", When: "true", Prompt: "Synthetic care activation", TimeZone: "UTC"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err = careEngine.Receive(ctx, care.Event{Source: "clock.minute", At: now, Data: map[string]any{}}, now); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot().CurrentTurn
	yes := true
	err = careEngine.Deliver(ctx, now, care.Presence{Awake: true, Unlocked: &yes}, s.Snapshot().CanSend, s.BackgroundReceipt, func(ctx context.Context, a care.Activation) (api.Receipt, error) {
		return s.Submit(ctx, api.Submission{ID: a.ID, Text: a.Prompt, Scheduled: true}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	for {
		v, err := s.WaitSnapshot(ctx, revision)
		if err != nil {
			t.Fatal(err)
		}
		revision = v.Revision
		if v.CurrentTurn != before && v.Phase == "completed" {
			break
		}
	}
	a := careEngine.Snapshot().Activations[0]
	if a.Status != "accepted" || s.BackgroundReceipt(a.ID).Outcome != "accepted" || !s.Snapshot().Scheduled {
		t.Fatal("care did not use native background projection")
	}
	t.Log("native provider requests contain metadata first, then only the explicitly loaded body/reference")
}
