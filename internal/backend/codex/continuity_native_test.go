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
)

func TestNativeContinuityApplicationContext(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_CODEX")
	if binary == "" {
		t.Skip("set CAELIS_BOT_TEST_CODEX for isolated native acceptance")
	}
	root := t.TempDir()
	home := filepath.Join(root, "codex-home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	var mu sync.Mutex
	var bodies []map[string]any
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Error(err)
			http.Error(w, "fixture", 400)
			return
		}
		mu.Lock()
		bodies = append(bodies, body)
		n := len(bodies)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); w.(http.Flusher).Flush() }
		id := fmt.Sprintf("continuity-%d", n)
		item := map[string]any{"id": id + "-item", "type": "message", "role": "assistant", "status": "completed", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": "Fixture continues the current task.", "annotations": []any{}}}}
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 70000, "output_tokens": 3, "total_tokens": 70003}}})
	}))
	defer provider.Close()
	config := fmt.Sprintf("model = \"continuity-fixture\"\nmodel_provider = \"continuity-fixture\"\n[model_providers.continuity-fixture]\nname = \"Local fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\nsupports_websockets = false\n", provider.URL)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewSession(SessionOptions{Binary: binary, Directory: root, StateFile: filepath.Join(root, "binding.json")})
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	defer func() {
		closeCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_ = s.Close(closeCtx)
	}()
	if err := s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	if s.binding.Continuity.Created.IsZero() {
		s.mu.Unlock()
		t.Fatal("native birth time missing")
	}
	s.opts.BotTools = &api.ToolConnection{}
	s.binding.Context.Injected = true
	// Policy time is synthetic; transport and provider fields are native.
	s.binding.Continuity = eligibleCues(s.binding.ThreadID, time.Now().Add(-time.Minute))
	s.binding.Continuity.Configuration = s.continuityConfiguration()
	original := s.binding.ThreadID
	s.mu.Unlock()
	prompt := "Continue my task.\n  Keep this exact text.  "
	r, err := s.Submit(ctx, api.Submission{ID: "native-continuity-one", Text: prompt}, nil)
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	for !s.Snapshot().CanSend {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	mu.Lock()
	first := bodies[0]
	mu.Unlock()
	var originalUser, privateDeveloper bool
	items, _ := first["input"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		role, _ := item["role"].(string)
		content, _ := item["content"].([]any)
		for _, rawPart := range content {
			part, _ := rawPart.(map[string]any)
			text, _ := part["text"].(string)
			if role == "user" && text == prompt {
				originalUser = true
			}
			if role == "developer" && strings.Contains(text, "Private Bot continuity invitation") {
				privateDeveloper = true
			}
		}
	}
	if !originalUser || !privateDeveloper {
		t.Fatalf("native separation: original user=%v private developer=%v", originalUser, privateDeveloper)
	}
	s.mu.Lock()
	observed := s.binding.Continuity.LatestInput
	accepted := s.binding.Continuity.Accepted
	current := s.binding.ThreadID
	s.mu.Unlock()
	if observed != 70000 || accepted.IsZero() || current != original {
		t.Fatal("native observation/acceptance/continued identity", observed, accepted, current)
	}
	r, err = s.Submit(ctx, api.Submission{ID: "native-continuity-two", Text: "Continue without renewal."}, nil)
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	for !s.Snapshot().CanSend {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	mu.Lock()
	second, _ := json.Marshal(bodies[len(bodies)-1]["input"])
	mu.Unlock()
	// The first fragment stays in native history, but is not injected again.
	if strings.Count(string(second), "Private Bot continuity invitation") != 1 {
		t.Fatal("native cooldown duplicated context")
	}
	t.Log("native request input=70000; original user content exact; cue is developer application context; synthetic reply continues same session")
}
