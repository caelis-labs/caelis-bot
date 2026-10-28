package codex

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
)

func TestNativeRefusalReachesDelegateWithoutLeakingPrivateTail(t *testing.T) {
	for _, stage := range []string{"thread/start", "initial-turn", "continuation", "steer"} {
		t.Run(stage, func(t *testing.T) {
			s, f, _, m := taskPair(t)
			sendSynthetic(t, s, "refusal-parent")
			args := map[string]string{"title": "Review", "prompt": "Synthetic intent"}
			method := "turn/start"
			if stage == "thread/start" {
				method = stage
			}
			if stage == "continuation" || stage == "steer" {
				task := newTask(t, m, "original-worker")
				args["task"] = task.ID
				delete(args, "title")
				if stage == "continuation" {
					finishTask(s, f, task.ID, "completed")
					awaitState(t, s, func(api.Snapshot) bool { return s.WorkStates()[0].Task.Status == "completed" })
				} else {
					method = "turn/steer"
				}
			}
			var calls atomic.Int32
			f.mu.Lock()
			previous := f.handle
			f.handle = func(w wireMessage) (any, bool) {
				if w.Method == method {
					calls.Add(1)
					return &NativeError{Code: -32602, Message: "native policy refused; token=private-secret"}, true
				}
				return previous(w)
			}
			f.mu.Unlock()
			r, err := bot.NewForRuntime(filepath.Join(t.TempDir(), "bot.json"), "codex", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.ConfigureTasks(m, nil); err != nil {
				t.Fatal(err)
			}
			ctx := api.WithToolInvocation(testContext(t), api.ToolInvocation{Provider: "codex", Session: "resident", Turn: "turn", CallID: "original-refusal"})
			raw, _ := json.Marshal(args)
			out := r.CallTool(ctx, "bot_delegate", raw)
			failure, ok := out.StructuredContent["error"].(map[string]string)
			if !ok || !out.IsError || failure["reason"] != "native_rejected" || failure["detail"] != "native policy refused; [private details omitted]" || calls.Load() != 1 {
				t.Fatalf("lost original refusal: %#v (calls %d)", out.StructuredContent, calls.Load())
			}
			encoded, _ := json.Marshal(out)
			if strings.Contains(string(encoded), "private-secret") {
				t.Fatal("private cause leaked")
			}
			// Repeating the exact invocation reads its receipt instead of dispatching.
			r.CallTool(ctx, "bot_delegate", raw)
			if calls.Load() != 1 {
				t.Fatal("original refusal replayed")
			}
		})
	}
}
