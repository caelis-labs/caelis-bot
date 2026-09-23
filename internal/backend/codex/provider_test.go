package codex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
)

func TestProviderRejectsInvalidExecutionBeforeConnecting(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir(), Execution: api.ExecutionSettings{ApprovalMode: "foreign-policy"}})
	called := false
	s.start = func(context.Context, Options) (*Client, error) { called = true; return nil, nil }
	if err := s.Connect(context.Background()); err == nil || called {
		t.Fatal("invalid policy reached a native connection")
	}
}
func TestToolGrantIsCopiedAndTranslatedOnlyIntoNamedApprovals(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir()})
	config := &api.ToolConnection{Command: "synthetic", Args: []string{"--bot-tools"}, Env: map[string]string{"credential": "synthetic"}, ApprovedTools: botpolicy.ApprovedTools()}
	if err := s.ConfigureBotTools(config); err != nil {
		t.Fatal(err)
	}
	config.Env["credential"] = "changed"
	config.ApprovedTools[0] = "foreign"
	config.Args[0] = "changed"
	p := s.connectionParams()["config"].(map[string]any)["mcp_servers.caelis_bot"].(map[string]any)
	if _, ok := p["default_tools_approval_mode"]; ok {
		t.Fatal("server-wide approval introduced")
	}
	policy := p["tools"].(map[string]any)
	if len(policy) != 9 || policy["foreign"] != nil || policy["bot_clock"] == nil || policy["bot_notebook"] != nil || policy["bot_memory"] == nil {
		t.Fatal("grant alias or scope leak")
	}
	if p["env"].(map[string]string)["credential"] != "synthetic" || p["args"].([]string)[0] != "--bot-tools" {
		t.Fatal("host mutation changed bound credentials")
	}
}
func TestUnknownStateCannotReplaceRuntime(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir()})
	s.state.Phase = "unknown"
	saved := false
	if _, err := s.ChangeRuntime(context.Background(), api.RuntimeSettings{Runtime: "codex"}, func() error { saved = true; return nil }); err == nil || saved {
		t.Fatal("unknown outcome permitted runtime replacement")
	}
}

func TestNotebookWorkspaceDoesNotLeakToWorkers(t *testing.T) {
	notebook := t.TempDir()
	worker := t.TempDir()
	s := NewSession(SessionOptions{Directory: t.TempDir()})
	c := &api.ToolConnection{Command: "synthetic", NotebookDirectory: notebook, Instructions: "resident-only-notebook-skill", WorkerInstructions: "worker instructions"}
	if err := s.ConfigureBotTools(c); err != nil {
		t.Fatal(err)
	}
	root := s.connectionParams()
	if root["cwd"] != notebook || root["runtimeWorkspaceRoots"].([]string)[0] != notebook {
		t.Fatal("resident cwd/root not Notebook")
	}
	turn := map[string]any{}
	s.applyExecution(turn, false)
	if turn["sandboxPolicy"].(map[string]any)["writableRoots"].([]string)[0] != notebook {
		t.Fatal("turn cannot write Notebook")
	}
	child := s.workerParams(worker, c.WorkerInstructions)
	b, _ := json.Marshal(child)
	if child["cwd"] != worker || strings.Contains(string(b), notebook) || strings.Contains(string(b), "resident-only-notebook-skill") {
		t.Fatal("worker inherited resident private context")
	}
}

func TestNotebookRefreshUsesResidentTurnLifecycle(t *testing.T) {
	s, f := sessionPair(t, "early-terminal")
	prepared := 0
	finished := make(chan struct{}, 4)
	s.op.Lock()
	s.mu.Lock()
	s.opts.BotTools = &api.ToolConnection{PrepareTurn: func(context.Context) error { prepared++; return nil }, FinishTurn: func() { _ = s.Snapshot(); finished <- struct{}{} }}
	s.mu.Unlock()
	s.op.Unlock()
	if got := sendSynthetic(t, s, "notebook-lifecycle"); got.Outcome != "accepted" {
		t.Fatal(got)
	}
	select {
	case <-finished:
	case <-testContext(t).Done():
		t.Fatal("completion did not refresh")
	}
	if prepared != 1 {
		t.Fatal("prepare count", prepared)
	}
	// A foreign completion is processed before the next resident completion; only
	// the latter may refresh. The channel acts as a deterministic event-loop fence.
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "foreign-worker", "turn": nativeTurn{ID: "other", Status: "completed"}})})
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "run-native", Status: "completed"}})})
	select {
	case <-finished:
	case <-testContext(t).Done():
		t.Fatal("no resident fence")
	}
	select {
	case <-finished:
		t.Fatal("worker triggered Notebook refresh")
	default:
	}
	s.op.Lock()
	s.mu.Lock()
	s.opts.BotTools.PrepareTurn = func(context.Context) error { return errors.New("unavailable notebook") }
	s.mu.Unlock()
	s.op.Unlock()
	got := sendSynthetic(t, s, "notebook-unavailable")
	if got.Outcome != "rejected" {
		t.Fatal("unavailable Notebook submitted")
	}
	f.mu.Lock()
	starts := f.starts
	f.mu.Unlock()
	if starts != 1 {
		t.Fatal("failed preparation reached native turn")
	}
}
