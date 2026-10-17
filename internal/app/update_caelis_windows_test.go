package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
)

// Opt-in real released Hosts and the full product assembly, with no UI, external
// model or user credentials. Installation bytes and visual acceptance are
// separate gates; this test exercises their PrepareUpdate/Close/reopen boundary.
func TestWindowsCaelisApplicationUpdatePreservesActiveWork(t *testing.T) {
	old, current := os.Getenv("CAELIS_BOT_TEST_PREVIOUS_BINARY"), os.Getenv("CAELIS_BOT_TEST_BINARY")
	if old == "" || current == "" {
		t.Skip("two explicitly selected native Caelis releases required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
	defer cancel()
	root := t.TempDir()
	for key, relative := range map[string]string{"HOME": "home", "USERPROFILE": "home", "APPDATA": "home/AppData/Roaming", "LOCALAPPDATA": "home/AppData/Local"} {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, path)
	}
	store, appRoot := filepath.Join(root, "store"), filepath.Join(root, "Bot")
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: old, CaelisStore: store}
	if _, err := caelisruntime.Manage(ctx, "start", old, store); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		command := exec.CommandContext(cleanup, current, "service", "stop", "--store-dir", store, "--format", "json")
		runtimeenv.BackgroundCommand(command)
		if _, err := command.CombinedOutput(); err != nil {
			t.Errorf("private Host cleanup: %v", err)
		}
	}()
	beforeHost, _, err := caelis.Discover(settings)
	if err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	entered := make(chan string, 2)
	var interrupted atomic.Int32
	var completed atomic.Int32
	var heldStarted atomic.Int32
	var sequence atomic.Int32
	var delegated atomic.Bool
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		var body map[string]any
		if json.Unmarshal(raw, &body) != nil {
			http.Error(w, "invalid fixture request", 400)
			return
		}
		marker := "CASE_APP_RESIDENT_HOLD"
		if body["model"] == "app-worker" {
			marker = "CASE_APP_WORKER_HOLD"
		}
		if !strings.Contains(string(raw), marker) {
			marker = ""
		}
		id := fmt.Sprintf("app-update-%d", sequence.Add(1))
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) { data, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", data) }
		if marker == "CASE_APP_RESIDENT_HOLD" && delegated.CompareAndSwap(false, true) {
			args := `{"title":"Application update worker","prompt":"CASE_APP_WORKER_HOLD"}`
			if _, responses := body["input"]; responses {
				emit(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": id, "type": "function_call", "call_id": id, "name": "bot_delegate"}})
				emit(map[string]any{"type": "response.function_call_arguments.delta", "item_id": id, "output_index": 0, "delta": args})
				emit(map[string]any{"type": "response.completed", "response": map[string]any{"model": body["model"], "status": "completed", "output": []any{map[string]any{"id": id, "type": "function_call", "call_id": id, "name": "bot_delegate", "arguments": args}}}})
			} else {
				emit(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": "bot_delegate", "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		if marker != "" {
			heldStarted.Add(1)
			select {
			case entered <- marker:
			default:
			}
			select {
			case <-release:
				completed.Add(1)
			case <-r.Context().Done():
				interrupted.Add(1)
				return
			}
		}
		text := "APPLICATION_UPDATE_COMPLETE"
		if _, responses := body["input"]; responses {
			emit(map[string]any{"type": "response.output_text.delta", "item_id": id, "output_index": 0, "delta": text})
			emit(map[string]any{"type": "response.completed", "response": map[string]any{"model": body["model"], "status": "completed", "output": []any{map[string]any{"id": id, "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}}}}})
		} else {
			emit(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}}})
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer model.Close()
	defer unblock()
	for _, name := range []string{"app-update", "app-worker"} {
		if err := caelis.ApplySetup(ctx, api.SetupRequest{Settings: settings, Action: "connect-model", Provider: "custom", Model: name, BaseURL: model.URL + "/v1", APIKey: "SYNTHETIC_ONLY"}); err != nil {
			t.Fatal(err)
		}
	}
	setup, err := caelis.InspectSetup(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	selector := ""
	workerSelector := ""
	for _, option := range setup.Models {
		if option.Label == "custom/app-update" {
			selector = option.Value
		}
		if option.Label == "custom/app-worker" {
			workerSelector = option.Value
		}
	}
	if selector == "" || workerSelector == "" {
		t.Fatal("native fixture model selector missing")
	}
	if err := localstate.WriteConfirmed(filepath.Join(appRoot, "runtime.json"), settings); err != nil {
		t.Fatal(err)
	}
	if err := localstate.WriteConfirmed(filepath.Join(appRoot, "providers", "caelis", "execution.json"), api.ExecutionSettings{Model: selector, ApprovalMode: "workspace-write"}); err != nil {
		t.Fatal(err)
	}
	if err := localstate.WriteConfirmed(filepath.Join(appRoot, "providers", "caelis", "work-execution.json"), api.WorkExecutionSettings{Model: workerSelector}); err != nil {
		t.Fatal(err)
	}
	open := func() *Application {
		t.Helper()
		a, err := newApplication(appRoot, Host{ResolveFiles: func(ids []string) ([]api.InputFile, error) {
			if len(ids) != 0 {
				return nil, fmt.Errorf("this fixture has no selected attachments")
			}
			return nil, nil
		}}, func(id string) (providerFactory, error) {
			factory, err := resolveProvider(id)
			if id == "caelis" && err == nil {
				// Keep this local lifecycle fixture's native approval explicit;
				// no synthetic model pretends to be the production Guardian.
				factory.Open = func(c providerConfig) (api.Engine, error) {
					return caelis.New(caelis.Options{RequireApproval: true, Diagnostics: c.Diagnostics, Directory: filepath.Dir(c.ConversationFile), Settings: c.Settings, Execution: c.Execution, WorkExecution: c.WorkExecution}), nil
				}
			}
			return factory, err
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = a.Close() })
		if err := a.Start(); err != nil {
			t.Fatal(err)
		}
		return a
	}
	wait := func(label string, condition func() bool) {
		t.Helper()
		for !condition() {
			select {
			case <-ctx.Done():
				t.Fatal(label, ctx.Err())
			case <-time.After(40 * time.Millisecond):
			}
		}
	}
	a := open()
	wait("initial application connection", func() bool { return a.Backend.Snapshot().CanSend })
	if _, err := a.Backend.InitializeBot(ctx, api.BotIntroduction{Name: "Update fixture"}); err != nil {
		t.Fatal(err)
	}
	wait("initial introduction accepted", func() bool { return a.Backend.BotInitialization().Status == "accepted" && a.Backend.Snapshot().CanSend })
	engine := a.currentEngine().(*caelis.Session)
	originalSession := engine.ConversationState().Session
	receipt, err := a.Backend.Submit(ctx, api.Submission{ID: "application-original-request", Text: "CASE_APP_RESIDENT_HOLD"})
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatalf("resident submission: %s %s %v", receipt.Outcome, receipt.Message, err)
	}
	wait("original native delegation approval", func() bool { return len(a.Backend.Snapshot().Approvals) > 0 })
	approval := a.Backend.Snapshot().Approvals[0]
	choice := ""
	for _, c := range approval.Choices {
		if c.Scope == "allow_once" {
			choice = c.ID
		}
	}
	if choice == "" {
		t.Fatal("native allow_once unavailable")
	}
	if err := a.Backend.Decide(ctx, api.Decision{ID: approval.ID, Choice: choice}); err != nil {
		t.Fatal(err)
	}
	wait("native owned Worker", func() bool { return len(a.tasks.ListTasks()) == 1 })
	task := a.tasks.ListTasks()[0]
	if task.Status == "failed" || task.Outcome == "rejected" {
		t.Fatal("native delegation was rejected")
	}
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case key := <-entered:
			seen[key] = true
		case <-ctx.Done():
			t.Fatal("both real requests were not active", ctx.Err())
		}
	}
	originalTarget, err := a.localWork.WorkTerminal(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(appRoot, "Notebook", "update-preserved.txt")
	if err := os.WriteFile(note, []byte("keep original notebook\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.PrepareUpdate(); err != nil {
		t.Fatal("active application update preparation", err)
	}
	if _, err := a.Backend.Submit(ctx, api.Submission{ID: "must-stay-fenced", Text: "must not execute"}); err == nil {
		t.Fatal("update admission remained open")
	}
	if err := a.Close(); err != nil {
		t.Fatal("application update detach", err)
	}
	afterClose, _, err := caelis.Discover(settings)
	if err != nil || afterClose.InstanceID != beforeHost.InstanceID || interrupted.Load() != 0 || completed.Load() != 0 {
		t.Fatal("application close changed active Host/turn ownership", err)
	}

	// The incoming app selects the newer release. Core must defer Host replacement
	// until original work finishes; Bot reopens the same persistent data meanwhile.
	if _, err := caelisruntime.Manage(ctx, "apply-update", current, store); err != nil {
		t.Fatal("native upgrade selection", err)
	}
	settings.CLIPath = current
	if err := localstate.WriteConfirmed(filepath.Join(appRoot, "runtime.json"), settings); err != nil {
		t.Fatal(err)
	}
	b := open()
	newEngine := b.currentEngine().(*caelis.Session)
	wait("restored original conversation", func() bool {
		return newEngine.ConversationState().Session == originalSession && b.Backend.Snapshot().CanInterrupt
	})
	restoredTarget, err := b.localWork.WorkTerminal(ctx, task.ID)
	if err != nil || restoredTarget.Session != originalTarget.Session || restoredTarget.Store != originalTarget.Store {
		t.Fatal("original Worker target changed", err)
	}
	if interrupted.Load() != 0 || heldStarted.Load() != 2 {
		t.Fatal("reopen cancelled or replayed original model requests")
	}
	unblock()
	wait("original work completion", func() bool { v, e := b.tasks.ReadTask(ctx, task.ID); return e == nil && v.Status == "completed" })
	wait("new Host selected", func() bool {
		d, _, e := caelis.Discover(settings)
		return e == nil && d.InstanceID != beforeHost.InstanceID
	})
	wait("restored Bot connected after upgrade", func() bool { return b.Backend.Snapshot().CanSend })
	wait("original Worker confirmed on new Host", func() bool {
		target, err := b.localWork.WorkTerminal(ctx, task.ID)
		discovery, _, discoverErr := caelis.Discover(settings)
		return err == nil && discoverErr == nil && discovery.InstanceID != beforeHost.InstanceID && target.Endpoint == discovery.Endpoint && target.Session == originalTarget.Session && target.Store == originalTarget.Store
	})
	beforeTurn := b.Backend.Snapshot().CurrentTurn
	if receipt, err := b.Backend.Submit(ctx, api.Submission{ID: "application-after-reconnect", Text: "CASE_APP_AFTER_RECONNECT"}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("new input did not reach restored Host", err)
	}
	wait("new request completed on restored Host", func() bool {
		v := b.Backend.Snapshot()
		return v.CanSend && v.CurrentTurn != beforeTurn && v.Phase == "completed"
	})
	if newEngine.ConversationState().Session != originalSession || interrupted.Load() != 0 {
		t.Fatal("Host replacement lost identity or interrupted original work")
	}
	if completed.Load() < 2 {
		t.Fatal("original requests did not complete")
	}
	if raw, err := os.ReadFile(note); err != nil || string(raw) != "keep original notebook\n" {
		t.Fatal("Notebook changed", err)
	}
	t.Log("full application assembly: active conversation + Worker preserved through PrepareUpdate/Close/reopen, new admission fenced, same Store/Session/Worker restored, original requests completed and new Host reconnected; no GUI/installer/model-service claim")
}
