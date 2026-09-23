package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// An external, pinned public binary exercises HTTP/SSE and durable Host storage.
// No sibling imports or real user credentials/data are involved.
func TestNativeHostIntegration(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY to run the external Host fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
	defer cancel()

	var delegate, continued, gesture, reminder atomic.Bool
	var continuationID atomic.Value
	var effects, reports atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		delta := map[string]any{"role": "assistant", "content": "CAELIS_BOT_FIXTURE_OK"}
		finish := "stop"
		tool := func(id, name string, args any, index int) map[string]any {
			b, _ := json.Marshal(args)
			return map[string]any{"index": index, "id": id, "type": "function", "function": map[string]string{"name": name, "arguments": string(b)}}
		}
		calls := []map[string]any{}
		if strings.Contains(body, "professional agent in an isolated managed work session") {
			if !strings.Contains(body, "work-command") {
				calls = append(calls, tool("work-command", "RunCommand", map[string]string{"command": "printf WORK_RESULT > result.txt; cat result.txt", "sandbox_permissions": "require_escalated", "justification": "Verify the fixture within its assigned directory"}, 0))
			}
		} else if strings.Contains(body, "DELEGATE_TWO") && delegate.CompareAndSwap(false, true) {
			for i := range 2 {
				calls = append(calls, tool(fmt.Sprintf("create-%d", i), "CreateWork", map[string]string{"assignment": fmt.Sprintf("Verify fixture report %d in its own directory", i)}, i))
			}
		} else if strings.Contains(body, "CONTINUE_ONE") && continued.CompareAndSwap(false, true) {
			calls = append(calls, tool("continue-one", "ContinueWork", map[string]string{"work_id": continuationID.Load().(string), "assignment": "Continue the fixture report in the same workspace"}, 0))
		} else if strings.Contains(body, "GESTURE_ONCE") && gesture.CompareAndSwap(false, true) {
			calls = append(calls, tool("gesture", "DesktopGesture", map[string]string{"gesture": "nod"}, 0))
		} else if strings.Contains(body, "REMINDER_ONCE") && reminder.CompareAndSwap(false, true) {
			calls = append(calls, tool("reminder", "DesktopReminders", map[string]string{"operation": "save", "id": "fixture-reminder", "label": "Fixture", "prompt": "Report the fixture reminder", "at": time.Now().Add(4 * time.Second).UTC().Format(time.RFC3339)}, 0))
		}
		if strings.Contains(body, "Report this completed work to the user once.") && len(calls) == 0 {
			reports.Add(1)
		}
		if len(calls) > 0 {
			delta = map[string]any{"role": "assistant", "tool_calls": calls}
			finish = "tool_calls"
		}
		payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
	}))
	defer provider.Close()
	root := t.TempDir()
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(root, "store")}
	cmd := exec.CommandContext(ctx, bin, "serve", "--store-dir", settings.CaelisStore, "--listen", "127.0.0.1:0")
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "CAELIS_") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	log, err := os.Create(filepath.Join(root, "host.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() }()
	var d discovery
	var token string
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		d, token, err = Discover(settings)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("Host did not publish discovery:", err)
		case <-tick.C:
		}
	}
	host, err := newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		Configuration struct {
			Revision string `json:"revision"`
		} `json:"configuration"`
	}
	if err = host.json(ctx, "GET", "/status", nil, &status, "", ""); err != nil {
		t.Fatal(err)
	}
	op := "configure-fixture"
	var result wire.CommandResult
	req := map[string]any{"operation_id": op, "expected_revision": status.Configuration.Revision, "config": map[string]string{"provider": "openai-compatible", "model": "gpt-4.1", "base_url": provider.URL + "/v1", "api_key": "SYNTHETIC_TEST_KEY"}}
	if err = host.json(ctx, "POST", "/configuration/connect-model", req, &result, op, status.Configuration.Revision); err != nil {
		t.Fatal(err)
	}
	if !succeeded(result.Outcome) {
		t.Fatalf("model configuration: %s", result.Outcome)
	}
	s := New(Options{Directory: filepath.Join(root, "bot"), Settings: settings})
	if err = s.BindDesktop(api.DesktopEffects{Execute: func(string, json.RawMessage) (json.RawMessage, error) {
		effects.Add(1)
		return json.RawMessage(`{"ok":true}`), nil
	}}); err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	if err = s.Connect(ctx); err != nil {
		var remote *remoteError
		if errors.As(err, &remote) {
			t.Fatalf("connect route=%s code=%s detail=%s", remote.path, remote.Code, remote.detail)
		}
		t.Fatal(err)
	}
	await := func(pred func(api.Snapshot) bool) api.Snapshot {
		t.Helper()
		for {
			snap := s.Snapshot()
			if pred(snap) {
				return snap
			}
			if _, err := s.WaitSnapshot(ctx, snap.Revision); err != nil {
				t.Fatalf("snapshot wait: %v (phase=%s issue=%s)", err, snap.Phase, snap.Message)
			}
		}
	}
	await(func(s api.Snapshot) bool { return s.CanSend })
	t.Log("binding ready")
	models, err := s.Models(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("model catalog returned", len(models))
	if len(models) == 0 {
		t.Fatal("model catalog empty")
	}
	r, err := s.Submit(ctx, api.Submission{ID: "fixture-prompt", Text: "Reply with the fixture sentinel."}, nil)
	if err != nil || r.Outcome != "accepted" {
		t.Fatalf("submit: %+v %v", r, err)
	}
	snap := await(func(s api.Snapshot) bool {
		for _, i := range s.Items {
			if strings.Contains(i.Text, "CAELIS_BOT_FIXTURE_OK") {
				return s.CanSend
			}
		}
		return false
	})
	if len(snap.Items) < 2 {
		t.Fatal("user/assistant projection missing")
	}
	if _, err := s.OwnedTasks(ctx); err != nil {
		t.Fatal(err)
	}

	// The two CreateWork calls come from the model, never inferred from prose.
	r, err = s.Submit(ctx, api.Submission{ID: "delegate-two", Text: "DELEGATE_TWO: create and verify two independent reports."}, nil)
	if err != nil || r.Outcome != "accepted" {
		t.Fatalf("delegate %v %v", r, err)
	}
	approved := map[string]bool{}
	for len(approved) < 2 {
		v := await(func(v api.Snapshot) bool {
			for _, a := range v.Approvals {
				if !approved[a.ID] {
					return true
				}
			}
			return false
		})
		if len(approved) == 0 {
			if !v.CanSend {
				t.Fatal("worker approval blocked secretary input")
			}
			receipt, e := s.Submit(ctx, api.Submission{ID: "secretary-during-work", Text: "Reply with a short status while the workers await approval."}, nil)
			if e != nil || receipt.Outcome != "accepted" {
				t.Fatal("secretary unavailable during work", receipt.Outcome, e)
			}
		}
		for _, a := range v.Approvals {
			if approved[a.ID] {
				continue
			}
			option := ""
			for _, c := range a.Choices {
				if c.Scope == "allow_once" {
					option = c.ID
				}
			}
			if option == "" {
				t.Fatalf("missing native approval choice: %+v", a.Choices)
			}
			if e := s.Decide(ctx, api.Decision{ID: a.ID, Choice: option}); e != nil {
				t.Fatal(e)
			}
			approved[a.ID] = true
		}
	}
	await(func(v api.Snapshot) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.completions) != 2 {
			return false
		}
		for _, n := range s.completions {
			if n.ReportState != "admitted" {
				return false
			}
		}
		if !v.CanSend {
			return false
		}
		for _, n := range s.completions {
			if n.ReportExecution.TurnId == v.CurrentTurn {
				return true
			}
		}
		return false
	})
	tasks, err := s.OwnedTasks(ctx)
	if err != nil || len(tasks) != 2 || tasks[0].Workspace == tasks[1].Workspace {
		t.Fatalf("isolated work projection: %d %v", len(tasks), err)
	}
	if tasks[0].Status != "completed" || tasks[1].Status != "completed" {
		t.Fatal("native success did not project terminal work")
	}
	if reports.Load() == 0 {
		t.Fatal("Control did not report")
	}
	if err = s.AcknowledgePresentation(ctx, s.Snapshot()); err != nil {
		t.Fatal(err)
	}
	t.Log("two model-owned workers, exact native approvals and Control report acknowledgement passed")
	continuationID.Store(tasks[0].ID)
	s.mu.Lock()
	var before wire.BotWork
	for _, w := range s.works {
		if w.Id == tasks[0].ID {
			before = w
		}
	}
	s.mu.Unlock()
	r, err = s.Submit(ctx, api.Submission{ID: "continue-one", Text: "CONTINUE_ONE: continue the first completed report."}, nil)
	if err != nil || r.Outcome != "accepted" {
		t.Fatal("continue prompt", r.Outcome, err)
	}
	await(func(v api.Snapshot) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !v.CanSend || len(s.completions) != 3 || len(s.works) != 2 {
			return false
		}
		for _, n := range s.completions {
			if n.ReportState != "admitted" {
				return false
			}
		}
		for _, w := range s.works {
			if w.Id == before.Id {
				return w.WorkspaceKey == before.WorkspaceKey && w.Execution.RunId != before.Execution.RunId && w.Status == "succeeded"
			}
		}
		return false
	})
	t.Log("secretary stayed available during work; continuation reused its workspace with a new execution")

	await(func(v api.Snapshot) bool { return v.CanSend })
	r, err = s.Submit(ctx, api.Submission{ID: "gesture-once", Text: "GESTURE_ONCE"}, nil)
	if err != nil || r.Outcome != "accepted" {
		t.Fatalf("gesture: %v %v", r, err)
	}
	await(func(v api.Snapshot) bool { return v.CanSend && effects.Load() == 1 })
	r, err = s.Submit(ctx, api.Submission{ID: "reminder-once", Text: "REMINDER_ONCE"}, nil)
	if err != nil || r.Outcome != "accepted" {
		t.Fatalf("reminder: %v %v", r, err)
	}
	await(func(v api.Snapshot) bool { return v.CanSend && effects.Load() == 2 })
	await(func(v api.Snapshot) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, j := range s.state.Operations {
			if strings.HasSuffix(j.Path, "/client/reminders/fire") && j.Outcome != "unknown" {
				return v.CanSend
			}
		}
		return false
	})
	if effects.Load() != 2 {
		t.Fatal("native effect executed more than once")
	}
	t.Log("scoped action claims, receipts and granted reminder fire passed")
	// Restart the actual Host, keeping its durable store and scoped client token.
	// A new process must resume observations without recreating the Bot/effects.
	s.mu.Lock()
	oldInstance, oldActivation, botID := s.state.InstanceID, s.state.Client.ActivationId, s.state.Bot.Id
	s.mu.Unlock()
	if err = cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal("Host shutdown:", err)
	}
	cmd = exec.CommandContext(ctx, bin, "serve", "--store-dir", settings.CaelisStore, "--listen", "127.0.0.1:0")
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "CAELIS_") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	await(func(v api.Snapshot) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return v.CanSend && s.state.InstanceID != oldInstance && s.state.Client.ActivationId != oldActivation
	})
	s.mu.Lock()
	sameBot := s.state.Bot.Id == botID
	s.mu.Unlock()
	if !sameBot || effects.Load() != 2 {
		t.Fatal("Host restart recreated identity or effects")
	}
	d, token, err = Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	host, err = newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Host restart retained Bot identity, renewed activation and did not replay effects")

	if err = s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var info wire.ServerInfo
	if err = host.json(ctx, "GET", "/initialize", nil, &info, "", ""); err != nil {
		t.Fatal("Bot exit stopped shared Host:", err)
	}
	t.Log("external Host: scoped binding, model catalog, prompt/SSE projection, explicit client exit passed")
}
