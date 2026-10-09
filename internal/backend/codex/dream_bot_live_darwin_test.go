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
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

func TestDreamMCPHelper(t *testing.T) {
	if os.Getenv("CAELIS_BOT_MCP_HELPER") == "" {
		t.Skip("only launched by the isolated app-server fixture")
	}
	if os.Getenv("FIXTURE_AUTH") != "synthetic-only" {
		os.Exit(3)
	}
	if err := bot.RunStdio(os.Stdin, os.Stdout); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

// This uses the installed app-server binary, a local model fixture and the
// real Bot MCP bridge. No user account or external model is contacted.
func TestNativeBotDreamRenewal(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_CODEX")
	if binary == "" {
		t.Skip("set CAELIS_BOT_TEST_CODEX to the installed CLI")
	}
	root := t.TempDir()
	home := filepath.Join(root, "codex-home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	var mu sync.Mutex
	var bodies []string
	var siblingEffects atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(req.Body, 4<<20))
		mu.Lock()
		bodies = append(bodies, string(body))
		n := len(bodies)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", b)
			w.(http.Flusher).Flush()
		}
		id := fmt.Sprintf("dream-bot-%d", n)
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		var item map[string]any
		if n == 1 {
			item = map[string]any{"id": "dream-bot-item", "type": "function_call", "namespace": "mcp__caelis_context", "name": "bot_dream", "call_id": "provider-dream-call", "arguments": `{"handoff":"Identity: fixture Bot. Objective: continue the exact original task. Original receipt: task-fixture-1 is pending. Next: answer the user after context renewal."}`}
		} else {
			item = map[string]any{"id": "dream-bot-final", "type": "message", "role": "assistant", "status": "completed", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": "fixture resumed", "annotations": []any{}}}}
		}
		output := []any{item}
		if n == 1 {
			output = append(output, map[string]any{"id": "dream-sibling-item", "type": "function_call", "namespace": "mcp__caelis_personal", "name": "bot_gesture", "call_id": "provider-sibling-call", "arguments": `{"action":"attention"}`})
		}
		for i, entry := range output {
			emit(map[string]any{"type": "response.output_item.done", "output_index": i, "item": entry})
		}
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": output, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	defer provider.Close()
	configuration := fmt.Sprintf("model = \"dream-fixture\"\nmodel_provider = \"dream-fixture\"\n[model_providers.dream-fixture]\nname = \"Local fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\nsupports_websockets = false\n", provider.URL)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	vault, err := notebook.OpenVault(filepath.Join(root, "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer vault.Close()
	resident, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "codex", func(string) error { siblingEffects.Add(1); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := resident.ConfigureDream(vault, ""); err != nil {
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
	config := bridge.Config(executable)
	config.Args = []string{"-test.run=^TestDreamMCPHelper$"}
	config.Env["CAELIS_BOT_MCP_HELPER"] = "1"
	config.Env["FIXTURE_AUTH"] = "synthetic-only"
	config.PrepareContext = func(context.Context) (api.ContextSeed, error) {
		return resident.PrepareHandoffContext(), nil
	}
	config.ConsumeContext = resident.ConsumeHandoffContext
	config.FinishTurn = func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = resident.RenewPendingToolHandoff(ctx)
		}()
	}
	s := NewSession(SessionOptions{Binary: binary, Directory: root, StateFile: filepath.Join(root, "binding.json"), BotTools: config})
	resident.Start(s)
	defer resident.Stop()
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	defer func() {
		closeCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = s.Close(closeCtx)
	}()
	if err := s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	old := s.ConversationState().Session
	oldGeneration := s.BotPluginGeneration()
	if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "first-input", Text: "Use bot_dream."}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	var renewed string
	for renewed == "" {
		state := s.ConversationState()
		if state.Session != "" && state.Session != old && resident.PrepareHandoffContext().HandoffDigest != "" {
			renewed = state.Session
			break
		}
		select {
		case <-time.After(25 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("Bot bridge did not renew original Turn", ctx.Err(), state)
		}
	}
	mu.Lock()
	firstCount := len(bodies)
	mu.Unlock()
	if firstCount != 1 {
		t.Fatalf("old Turn continued to another model step: %d", firstCount)
	}
	if siblingEffects.Load() != 0 {
		t.Fatalf("same-batch later Bot effect ran after terminal tool: %d", siblingEffects.Load())
	}
	if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "second-input", Text: "Continue the original task."}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	for {
		mu.Lock()
		got := append([]string(nil), bodies...)
		mu.Unlock()
		if len(got) >= 2 {
			if !strings.Contains(got[1], "Original receipt: task-fixture-1") || strings.Contains(got[1], "provider-dream-call") || strings.Contains(got[0], "Original receipt: task-fixture-1") {
				t.Fatalf("new first prompt did not isolate private handoff: old=%t new=%t oldCall=%t", strings.Contains(got[0], "Original receipt: task-fixture-1"), strings.Contains(got[1], "Original receipt: task-fixture-1"), strings.Contains(got[1], "provider-dream-call"))
			}
			break
		}
		select {
		case <-time.After(25 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("new Session did not receive the next user input", ctx.Err())
		}
	}
	if renewed == old || s.BotPluginGeneration() == oldGeneration || len(s.binding.PastThreads) == 0 || s.binding.PastThreads[len(s.binding.PastThreads)-1] != old {
		t.Fatal("native thread history or renewal receipt missing")
	}
	servers := s.connectionParams()["config"].(map[string]any)
	for _, name := range []string{"caelis_context", "caelis_personal"} {
		service, ok := servers["mcp_servers."+name].(map[string]any)
		if !ok || service["env"].(map[string]string)["FIXTURE_AUTH"] != "synthetic-only" {
			t.Fatal("new thread lost retained MCP configuration", name)
		}
	}
}
