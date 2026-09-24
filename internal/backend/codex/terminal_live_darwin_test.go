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
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

// Opt-in installed-runtime evidence. The production adapter and owned process
// are real; only the Responses provider is synthetic. No personal auth/config.
func TestNativeSharedWorkerTerminal(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_CODEX")
	if binary == "" {
		t.Skip("set CAELIS_BOT_TEST_CODEX to the installed CLI")
	}
	dir := t.TempDir()
	home := filepath.Join(dir, "codex-home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	rootStarted, releaseRoot := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 4<<20))
		n := calls.Add(1)
		id := fmt.Sprintf("local-%d", n)
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); w.(http.Flusher).Flush() }
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		if n == 1 {
			close(rootStarted)
			select {
			case <-releaseRoot:
			case <-r.Context().Done():
				return
			}
		}
		item := map[string]any{"id": id + "-message", "type": "message", "role": "assistant", "status": "completed", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": id + "-ack", "annotations": []any{}}}}
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	defer provider.Close()
	defer close(releaseRoot)
	config := fmt.Sprintf("model = \"bubble-fixture\"\nmodel_provider = \"bubble-fixture\"\n[model_providers.bubble-fixture]\nname = \"Isolated local fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\nsupports_websockets = false\n", provider.URL)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Second)
	defer cancel()
	s := NewSession(SessionOptions{Binary: binary, Directory: filepath.Join(dir, "Work"), StateFile: filepath.Join(dir, "conversation.json"), WorkRoot: filepath.Join(dir, "Tasks")})
	defer func() {
		c, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := s.Close(c); err != nil {
			t.Error(err)
		}
	}()
	if err := s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(ctx, api.Submission{ID: "shared-terminal-root", Text: "Run an isolated synthetic worker for terminal acceptance."}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rootStarted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	m, err := tasks.Open(filepath.Join(dir, "tasks.json"), s.workRoot(), "codex", s, s, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	work, err := m.StartTask(ctx, api.TaskStart{RequestID: "shared-terminal-worker", Title: "Terminal acceptance", Prompt: "验证同一任务：Bot 和终端用户都能发送消息并观察进展。"})
	if err != nil {
		t.Fatal(err)
	}
	waitResult := func(result string) {
		t.Helper()
		var revision uint64
		for {
			states := s.WorkStates()
			if len(states) == 1 && states[0].Task.Status == "completed" && states[0].Task.Result == result {
				return
			}
			snapshot, err := s.WaitSnapshot(ctx, revision)
			if err != nil {
				t.Fatalf("worker result %s: %v; state=%+v", result, err, states)
			}
			revision = snapshot.Revision
		}
	}
	waitResult("local-2-ack")
	target, err := m.WorkTerminal(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimPrefix(target.Endpoint, "unix://")
	for _, p := range []string{path, filepath.Dir(path)} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatal("runtime socket not private", err)
		}
	}
	human, err := Start(ctx, Options{Binary: binary, Socket: path, Experimental: true, HandleRequests: true})
	if err != nil {
		t.Fatal(err)
	}
	defer human.Close()
	if !human.UsesSharedServer() || s.client.UsesSharedServer() {
		t.Fatal("process ownership conflated")
	}
	var resumed struct {
		Thread nativeThread `json:"thread"`
	}
	if err := callDecode(ctx, human, "thread/resume", map[string]any{"threadId": target.Thread}, &resumed); err != nil || resumed.Thread.ID != target.Thread {
		t.Fatal("human cannot attach", err)
	}
	var started struct {
		Turn nativeTurn `json:"turn"`
	}
	if err := callDecode(ctx, human, "turn/start", map[string]any{"threadId": target.Thread, "input": []nativeInput{{Type: "text", Text: "The human starts another turn in this same worker", TextElements: []any{}}}}, &started); err != nil {
		t.Fatal(err)
	}
	waitResult("local-3-ack")
	human.Close()
	if _, err := m.SendTask(ctx, api.TaskMessage{ID: work.ID, RequestID: "bot-after-human-detach", Prompt: "Bot continues after terminal observer closes"}); err != nil {
		t.Fatal(err)
	}
	waitResult("local-4-ack")
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("owned runtime socket leaked")
	}
	t.Log("real owned Unix App Server: Bot completion, human turn, human disconnect, Bot continuation, and owned process cleanup passed")
}
