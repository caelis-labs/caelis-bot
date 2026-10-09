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
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botskills"
	"github.com/caelis-labs/caelis-bot/internal/care"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
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
	vault, err := notebook.OpenVault(filepath.Join(dir, "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer vault.Close()
	os.WriteFile(filepath.Join(vault.Path(), "MEMORY.md"), []byte("# Memory\nNative core identity."), 0600)
	handoffPath := filepath.Join(vault.Path(), notebook.HandoffName)
	var mu sync.Mutex
	var requests []string
	workerRequested := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		mu.Lock()
		requests = append(requests, string(body))
		n := len(requests)
		mu.Unlock()
		if n == 6 {
			defer close(workerRequested)
		}
		id := fmt.Sprintf("skill-%d", n)
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); w.(http.Flusher).Flush() }
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		var item map[string]any
		if n == 2 {
			var items []any
			for index, ref := range []string{"desktop-observation.md", "desktop-actions.md", "desktop-images.md", "desktop-recovery.md", "telegram-setup.md"} {
				path := filepath.Join(filepath.Dir(skillPath), "references", ref)
				args, _ := json.Marshal(map[string]any{"cmd": "cat '" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'", "max_output_tokens": 8000})
				item := map[string]any{"id": fmt.Sprintf("%s-%d", id, index), "type": "function_call", "name": "exec_command", "call_id": fmt.Sprintf("%s-%d", id, index), "arguments": string(args)}
				items = append(items, item)
				emit(map[string]any{"type": "response.output_item.done", "output_index": index, "item": item})
			}
			emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": items, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
			return
		}
		if n == 1 {
			p := skillPath
			command := "cat '" + strings.ReplaceAll(p, "'", "'\"'\"'") + "'"
			args, _ := json.Marshal(map[string]any{"cmd": command, "max_output_tokens": 8000})
			item = map[string]any{"id": id, "type": "function_call", "name": "exec_command", "call_id": id, "arguments": string(args)}
		} else if n == 4 {
			item = map[string]any{"id": id, "type": "function_call", "namespace": "mcp__caelis_context", "name": "bot_dream", "call_id": "native-skill-dream-call", "arguments": `{"handoff":"Identity: Caelis Bot. Current goal: finish the user's skill check. The desktop guide and references were read. No effect is pending. Next: answer the next user message from a fresh context."}`}
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
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	resident, err := bot.NewForRuntime(filepath.Join(dir, "bot.json"), "codex", nil)
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
	tools := bridge.Config(executable)
	tools.Args = []string{"-test.run=^TestDreamMCPHelper$"}
	tools.Env["CAELIS_BOT_MCP_HELPER"] = "1"
	tools.Env["FIXTURE_AUTH"] = "synthetic-only"
	tools.Instructions = botskills.Instructions(skillPath)
	tools.PrepareContext = func(ctx context.Context) (api.ContextSeed, error) {
		seed, err := vault.PrepareContext(ctx)
		if err != nil {
			return api.ContextSeed{}, err
		}
		handoff := resident.PrepareHandoffContext()
		seed.Text += handoff.Text
		seed.HandoffDigest = handoff.HandoffDigest
		return seed, nil
	}
	tools.ConsumeContext = resident.ConsumeHandoffContext
	tools.FinishTurn = func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = resident.RenewPendingToolHandoff(ctx)
		}()
	}
	s := NewSession(SessionOptions{Binary: binary, Directory: vault.Path(), StateFile: filepath.Join(dir, "binding.json"), BotTools: tools})
	resident.Start(s)
	defer resident.Stop()
	defer func() {
		c, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = s.Close(c)
	}()
	if err = s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := resident.SubmitUser(ctx, api.Submission{ID: "progressive-skill-input", Text: "Read the applicable guide and its desktop reference."}, nil)
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
	if !strings.Contains(initial[1], "# Restore your context") || strings.Contains(initial[1], "# Observe the desktop narrowly") || !strings.Contains(initial[2], "# Observe the desktop narrowly") {
		t.Fatal("native file tools did not progressively load skill")
	}
	if strings.Contains(initial[1], "# Perform known desktop actions") || !strings.Contains(initial[2], "# Perform known desktop actions") {
		t.Fatal("desktop policy reference was not progressively loaded")
	}
	for _, marker := range []string{"# Capture explicit evidence", "# Read the original desktop receipt", "# Help the user connect Telegram"} {
		if strings.Contains(initial[1], marker) || !strings.Contains(initial[2], marker) {
			t.Fatal("conditional guide did not load", marker)
		}
	}
	correlated := false
	for _, item := range s.Snapshot().Items {
		if item.Kind == "user" && item.RequestID == "progressive-skill-input" {
			correlated = true
			if item.Text != "Read the applicable guide and its desktop reference." {
				t.Fatal("context prefix leaked into native chat projection")
			}
		}
	}
	if !correlated {
		t.Fatal("native user input did not preserve submission identity")
	}
	if !strings.Contains(initial[1], "Keep your Notebook and `MEMORY.md` useful during") || !strings.Contains(initial[1], "use `bot_dream` when it is available") {
		t.Fatal("new bot-core memory or context guidance did not load progressively")
	}
	old := s.ConversationState().Session
	r, err = resident.SubmitUser(ctx, api.Submission{ID: "native-dream", Text: "Continue the skill check and renew your context with bot_dream."}, nil)
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	for {
		state := s.ConversationState()
		if state.Session != "" && state.Session != old && resident.PrepareHandoffContext().HandoffDigest != "" {
			break
		}
		select {
		case <-time.After(25 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("native bot_dream did not renew the original Turn", ctx.Err(), state)
		}
	}
	mu.Lock()
	dreamRequests := append([]string(nil), requests...)
	mu.Unlock()
	if len(dreamRequests) != 4 || !strings.Contains(dreamRequests[3], "bot_dream") {
		t.Fatal("old Turn continued or resident bot_dream was not called")
	}
	if _, err = os.Stat(handoffPath); !os.IsNotExist(err) {
		t.Fatal("bot_dream wrote the retired Notebook HANDOFF.md")
	}
	r, err = resident.SubmitUser(ctx, api.Submission{ID: "after-native-dream", Text: "A new topic."}, nil)
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	for !s.Snapshot().CanSend {
		v, e := s.WaitSnapshot(ctx, revision)
		if e != nil {
			t.Fatal(e)
		}
		revision = v.Revision
	}
	mu.Lock()
	nextRequests := append([]string{}, requests...)
	mu.Unlock()
	if len(nextRequests) != 5 || !strings.Contains(nextRequests[4], "Native core identity.") || !strings.Contains(nextRequests[4], "No effect is pending.") || strings.Contains(nextRequests[4], "Read the applicable guide and its desktop reference.") {
		t.Fatal("new session context did not reset and restore")
	}
	if resident.PrepareHandoffContext().HandoffDigest != "" {
		t.Fatal("accepted private handoff was not consumed")
	}
	workerDir := filepath.Join(dir, "Tasks", "isolated")
	if err = os.MkdirAll(workerDir, 0700); err != nil {
		t.Fatal(err)
	}
	workerParams := s.workerParams(workerDir, "Complete the assigned task.", &taskRecord{})
	if contextTool := workerParams["config"].(map[string]any)["mcp_servers.caelis_context"].(map[string]any); contextTool["enabled"] != false {
		t.Fatal("resident bot_dream service was not disabled for the Worker")
	}
	var worker threadExecutionResponse
	if err = callDecode(ctx, s.client, "thread/start", workerParams, &worker); err != nil {
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
	workerRequest := requests[5]
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
	result := s.BackgroundResult(a.ID)
	if !result.Complete || !result.Visible {
		t.Fatal("native visible result was not retained", result)
	}
	if err = careEngine.Deliver(ctx, time.Now(), care.Presence{}, false, s.BackgroundReceipt, nil, s.BackgroundResult); err != nil {
		t.Fatal(err)
	}
	if b := careEngine.Budget(time.Now()); b.InterruptionsUsed != 1 || b.Reserved != 0 {
		t.Fatal("native visible result not accounted", b)
	}
	t.Log("native provider requests contain metadata first, then only the explicitly loaded body/reference")
}
