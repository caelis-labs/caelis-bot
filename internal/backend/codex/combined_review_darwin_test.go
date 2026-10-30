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
	"github.com/caelis-labs/caelis-bot/internal/backend/continuity"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botskills"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

// This uses the installed app-server binary, a local model fixture and the
// real Bot MCP bridge. No user account or external model is contacted.
// Temporary combination acceptance: native protocols and file tools are real;
// age/activity and model choices are scripted, not autonomous-model evidence.
func TestNativeCombinedReview(t *testing.T) {
	for _, decision := range []string{"defer", "dream"} {
		t.Run(decision, func(t *testing.T) { nativeCombinedReview(t, decision == "dream") })
	}
}
func nativeCombinedReview(t *testing.T, dream bool) {
	binary := os.Getenv("CAELIS_BOT_TEST_CODEX")
	if binary == "" {
		t.Skip("set CAELIS_BOT_TEST_CODEX to the installed CLI")
	}
	root := t.TempDir()
	skillPath, err := botskills.Install(root)
	if err != nil {
		t.Fatal(err)
	}
	memoryReference := filepath.Join(filepath.Dir(skillPath), "references", "memory.md")
	body, err := os.ReadFile(memoryReference)
	if err != nil {
		t.Fatal(err)
	}
	const guideMarker = "combined-review-reference-loaded"
	if err = os.WriteFile(memoryReference, append(body, []byte("\n"+guideMarker+"\n")...), 0600); err != nil {
		t.Fatal(err)
	}

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
		if n == 2 {
			args, _ := json.Marshal(map[string]any{"cmd": "cat " + fmt.Sprintf("%q", memoryReference), "max_output_tokens": 6000})
			item = map[string]any{"id": "read-reference", "type": "function_call", "name": "exec_command", "call_id": "read-reference", "arguments": string(args)}
		} else if n == 3 && dream {
			item = map[string]any{"id": "dream-bot-item", "type": "function_call", "namespace": "mcp__caelis_context", "name": "bot_dream", "call_id": "provider-dream-call", "arguments": `{"handoff":"Identity: fixture Bot. Preference: short replies. Goal: original task; no deployment. Original task handle: task-123. Authorization: read only; deployment revoked. Unknown receipt: receipt-456; reconcile only. Review path: review-notes.md; reviewed src/a; unread src/b. Next: inspect original Worker question."}`}
		} else {
			reply := "fixture resumed"
			if n == 3 && !dream {
				reply = api.SilentReminder
			}
			item = map[string]any{"id": "dream-bot-final", "type": "message", "role": "assistant", "status": "completed", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": reply, "annotations": []any{}}}}
		}
		output := []any{item}
		if n == 3 && dream {
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
	if err = os.WriteFile(filepath.Join(vault.Path(), "MEMORY.md"), []byte("Identity: fixture Bot. Stable preference: short replies. Standing constraint: ask before publishing.\n"), 0600); err != nil {
		t.Fatal(err)
	}
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
	config.Instructions = botskills.Bootstrap()
	config.SkillInstructions = botskills.Instructions(skillPath)
	config.BuiltinSkillRoots = []string{filepath.Dir(skillPath)}
	config.Args = []string{"-test.run=^TestDreamMCPHelper$"}
	config.Env["CAELIS_BOT_MCP_HELPER"] = "1"
	config.Env["FIXTURE_AUTH"] = "synthetic-only"
	config.PrepareContext = func(ctx context.Context) (api.ContextSeed, error) {
		seed, err := vault.PrepareContext(ctx)
		if err != nil {
			return api.ContextSeed{}, err
		}
		handoff := resident.PrepareHandoffContext()
		seed.Text += handoff.Text
		seed.HandoffDigest = handoff.HandoffDigest
		return seed, nil
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
	if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "first-input", Text: "Continue the original task within read-only authorization."}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}

	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
	stamp, observed := time.Now(), s.ConversationState()
	s.mu.Lock()
	s.binding.Continuity.Observation = continuity.Observation{Session: observed.Session, Turn: observed.Turn, Since: stamp.Add(-14 * 24 * time.Hour), CompletedAt: stamp, Turns: 64, Compactions: 3, Maintenance: [8]continuity.MaintenanceTurn{{Turn: 62, At: stamp}, {Turn: 63, At: stamp}, {Turn: 64, At: stamp}}}
	s.mu.Unlock()
	for range 2 {
		if err := resident.ReviewContinuity(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if !dream {
		awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend && v.Quiet })
		if s.ConversationState().Session != old || resident.PrepareHandoffContext().HandoffDigest != "" {
			t.Fatal("healthy deferral renewed")
		}
		mu.Lock()
		got := append([]string(nil), bodies...)
		mu.Unlock()
		if len(got) != 3 || strings.Contains(got[1], guideMarker) || !strings.Contains(got[2], guideMarker) || !strings.Contains(got[1], "Bot system event: continuity_review") {
			t.Fatal("native reference read or invitation missing")
		}
		if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "healthy-next", Text: "Keep working; no broader authorization."}, nil); err != nil || receipt.Outcome != "accepted" {
			t.Fatal(receipt, err)
		}
		awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
		before := len(got) + 1
		stamp, observed = time.Now(), s.ConversationState()
		s.mu.Lock()
		s.binding.Continuity.Observation = continuity.Observation{Session: observed.Session, Turn: observed.Turn, Since: stamp.Add(-14 * 24 * time.Hour), CompletedAt: stamp, Turns: 128, Compactions: 6, Maintenance: [8]continuity.MaintenanceTurn{{Turn: 126, At: stamp}, {Turn: 127, At: stamp}, {Turn: 128, At: stamp}}}
		s.mu.Unlock()
		resident.Close()
		reopened, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "codex", nil)
		if err != nil {
			t.Fatal(err)
		}
		reopened.Start(s)
		defer reopened.Close()
		for range 2 {
			if err := reopened.ReviewContinuity(ctx); err != nil {
				t.Fatal(err)
			}
		}
		mu.Lock()
		count := len(bodies)
		mu.Unlock()
		if count != before {
			t.Fatal("restart repeated invitation", count, before)
		}
		t.Log("native reference read -> quiet deferral -> ordinary work; restart cooldown preserved; no Dream")
		return
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
	if firstCount != 3 {
		t.Fatalf("old Turn continued to another model step: %d", firstCount)
	}
	if siblingEffects.Load() != 0 {
		t.Fatalf("same-batch later Bot effect ran after terminal tool: %d", siblingEffects.Load())
	}
	if receipt, err := resident.SubmitSystem(ctx, api.SystemMessage{ID: "second-input", Source: api.SystemWorkerApproval, Text: "An original Worker approval is pending; inspect its original record."}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	for {
		mu.Lock()
		got := append([]string(nil), bodies...)
		mu.Unlock()
		if len(got) >= 4 {
			if !strings.Contains(got[3], "Unknown receipt: receipt-456") || strings.Contains(got[3], "provider-dream-call") || strings.Contains(got[0], "Unknown receipt: receipt-456") {
				t.Fatalf("new first prompt did not isolate private handoff: old=%t new=%t oldCall=%t", strings.Contains(got[0], "Unknown receipt: receipt-456"), strings.Contains(got[3], "Unknown receipt: receipt-456"), strings.Contains(got[3], "provider-dream-call"))
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
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
	if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "third-input", Text: "Keep the same task and its original limits."}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
	mu.Lock()
	got := append([]string(nil), bodies...)
	mu.Unlock()
	if len(got) != 5 || strings.Count(got[4], "[Private Bot handoff:") != 1 || strings.Count(got[4], "[Bot context:") != 1 || resident.PrepareHandoffContext().HandoffDigest != "" {
		t.Fatalf("native Codex seed: requests=%d handoff=%d core=%d consumed=%t", len(got), strings.Count(got[len(got)-1], "[Private Bot handoff:"), strings.Count(got[len(got)-1], "[Bot context:"), resident.PrepareHandoffContext().HandoffDigest == "")
	}

	if strings.Contains(got[1], guideMarker) || !strings.Contains(got[2], guideMarker) || !strings.Contains(got[1], "Bot system event: continuity_review") {
		t.Fatal("Dream did not follow native reference read")
	}
	for _, fact := range []string{"task-123", "read only", "deployment revoked", "receipt-456", "review-notes.md", "unread src/b"} {
		if !strings.Contains(got[3], fact) {
			t.Fatal("handoff lost supplied fact", fact)
		}
	}
	t.Log("native reminder -> reference read -> Dream -> Worker approval first seed -> one-time seed; supplied task/auth/review facts retained")
	servers := s.connectionParams()["config"].(map[string]any)
	for _, name := range []string{"caelis_context", "caelis_personal"} {
		service, ok := servers["mcp_servers."+name].(map[string]any)
		if !ok || service["env"].(map[string]string)["FIXTURE_AUTH"] != "synthetic-only" {
			t.Fatal("new thread lost retained MCP configuration", name)
		}
	}
}
