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
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
)

type modelStep struct {
	Reply   string
	Name    string
	Args    any
	Block   <-chan struct{}
	Entered chan<- struct{}
}
type acceptanceModel struct {
	mu       sync.Mutex
	plans    map[string][]modelStep
	counts   map[string]int
	requests map[string][]map[string]any
	sequence int
	failure  string
}

var casePattern = regexp.MustCompile(`CASE_[A-Z_]+`)

func newAcceptanceModel() *acceptanceModel {
	return &acceptanceModel{plans: map[string][]modelStep{}, counts: map[string]int{}, requests: map[string][]map[string]any{}}
}
func (m *acceptanceModel) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	cases := casePattern.FindAllString(string(raw), -1)
	key := ""
	if len(cases) > 0 {
		key = cases[len(cases)-1]
	}
	m.mu.Lock()
	index := m.counts[key]
	m.counts[key]++
	m.requests[key] = append(m.requests[key], body)
	m.sequence++
	seq := m.sequence
	var step modelStep
	if index < len(m.plans[key]) {
		step = m.plans[key][index]
	}
	m.mu.Unlock()
	if step.Entered != nil {
		select {
		case step.Entered <- struct{}{}:
		default:
		}
	}
	if step.Block != nil {
		select {
		case <-step.Block:
		case <-r.Context().Done():
			return
		}
	}
	args, _ := json.Marshal(step.Args)
	if strings.Contains(string(args), "$RESOURCE_PATH") {
		path := regexp.MustCompile(`\.resources/[a-f0-9]{64}`).FindString(string(raw))
		if path == "" {
			m.mu.Lock()
			m.failure = "missing native resource path"
			m.mu.Unlock()
		}
		args = []byte(strings.ReplaceAll(string(args), "$RESOURCE_PATH", path))
	}
	reply := step.Reply
	if reply == "" {
		reply = "ACCEPTANCE_COMPLETE"
	}
	id := fmt.Sprintf("fixture-%d", seq)
	if step.Name == "FixtureLookup" {
		id = "provider-reused-id"
	}
	w.Header().Set("Content-Type", "text/event-stream")
	emit := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b) }
	if _, responses := body["input"]; responses {
		if step.Name != "" {
			emit(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": id, "type": "function_call", "call_id": id, "name": step.Name}})
			emit(map[string]any{"type": "response.function_call_arguments.delta", "item_id": id, "output_index": 0, "delta": string(args)})
			emit(map[string]any{"type": "response.completed", "response": map[string]any{"model": body["model"], "status": "completed", "output": []any{map[string]any{"id": id, "type": "function_call", "call_id": id, "name": step.Name, "arguments": string(args)}}}})
		} else {
			emit(map[string]any{"type": "response.output_text.delta", "item_id": id, "output_index": 0, "delta": reply})
			emit(map[string]any{"type": "response.completed", "response": map[string]any{"model": body["model"], "status": "completed", "output": []any{map[string]any{"id": id, "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": reply}}}}}})
		}
	} else {
		delta := map[string]any{"role": "assistant", "content": reply}
		finish := "stop"
		if step.Name != "" {
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": step.Name, "arguments": string(args)}}}}
			finish = "tool_calls"
		}
		emit(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}
func (m *acceptanceModel) set(key string, steps ...modelStep) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans[key] = steps
}
func (m *acceptanceModel) seen(key string) []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return clone(m.requests[key])
}

type acceptanceTools struct {
	defs []api.ToolDefinition
	call func(context.Context, string, json.RawMessage) api.ToolResult
}

func (h *acceptanceTools) Definitions() []api.ToolDefinition { return h.defs }
func (h *acceptanceTools) CallTool(ctx context.Context, name string, args json.RawMessage) api.ToolResult {
	return h.call(ctx, name, args)
}
func fixtureDefinitions(kind string) []api.ToolDefinition {
	out := []api.ToolDefinition{}
	for _, name := range []string{"FixtureLookup", "FixtureDelegate", "FixtureSchedule"} {
		out = append(out, api.ToolDefinition{Name: name, Description: "Controlled acceptance tool", InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"key":{"type":%q}},"additionalProperties":false}`, kind))})
	}
	return out
}
func waitAcceptance(t *testing.T, ctx context.Context, check func() bool) {
	t.Helper()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for !check() {
		select {
		case <-ctx.Done():
			t.Fatal("acceptance condition timed out")
		case <-tick.C:
		}
	}
}
func waitTurn(t *testing.T, ctx context.Context, s *Session, previous string) {
	t.Helper()
	waitAcceptance(t, ctx, func() bool {
		v := s.Snapshot()
		return v.CurrentTurn != "" && v.CurrentTurn != previous && v.CanSend && (v.Phase == "completed" || v.Phase == "failed" || v.Phase == "interrupted")
	})
	if v := s.Snapshot(); v.Phase != "completed" {
		t.Fatalf("turn ended %s: %s", v.Phase, v.Message)
	}
}
func submitAcceptance(t *testing.T, ctx context.Context, s *Session, key string) {
	t.Helper()
	before := s.Snapshot().CurrentTurn
	r, e := s.Submit(ctx, api.Submission{ID: strings.ToLower(key), Text: key}, nil)
	if e != nil || r.Outcome != "accepted" {
		t.Fatalf("submit %s: %+v %v", key, r, e)
	}
	waitTurn(t, ctx, s, before)
}

func TestNativeHostIntegration(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY for isolated external Host acceptance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	model := newAcceptanceModel()
	server := httptest.NewServer(http.HandlerFunc(model.serve))
	defer server.Close()
	root := t.TempDir()
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(root, "store")}
	if e := os.Mkdir(filepath.Join(root, "home"), 0700); e != nil {
		t.Fatal(e)
	}
	var cmd *exec.Cmd
	start := func() {
		cmd = exec.CommandContext(ctx, bin, "serve", "--store-dir", settings.CaelisStore, "--listen", "127.0.0.1:0")
		cmd.Dir = root
		cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + filepath.Join(root, "home"), "TMPDIR=" + os.TempDir()}
		log, e := os.OpenFile(filepath.Join(root, "host.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		cmd.Stdout = log
		cmd.Stderr = log
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		log.Close()
		waitAcceptance(t, ctx, func() bool { _, _, e := Discover(settings); return e == nil })
	}
	stop := func() {
		if cmd != nil {
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
			cmd = nil
		}
	}
	start()
	defer stop()
	d, token, e := Discover(settings)
	if e != nil {
		t.Fatal(e)
	}
	host, e := newClient(d.Endpoint, token)
	if e != nil {
		t.Fatal(e)
	}
	info, e := initialize(ctx, host)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range required {
		found := false
		for _, v := range info.Capabilities {
			if c == v {
				found = true
			}
		}
		if !found {
			t.Fatal("missing capability", c)
		}
	}
	for _, name := range []string{"gpt-5.4-mini", "gpt-5.4"} {
		var status wire.StatusSnapshot
		if e = host.json(ctx, "GET", "/status", nil, &status, "", ""); e != nil {
			t.Fatal(e)
		}
		op := "configure-" + name
		var result wire.CommandResult
		e = host.json(ctx, "POST", "/configuration/connect-model", wire.ConnectModelRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Config: wire.ConnectConfig{Provider: "openai", Model: name, BaseUrl: pointer(server.URL + "/v1"), ApiKey: pointer("SYNTHETIC_ONLY")}}, &result, op, string(status.Configuration.Revision))
		if e != nil || !succeeded(result.Outcome) {
			t.Fatalf("configure: %+v %v", result, e)
		}
	}
	vault, e := notebook.OpenVault(filepath.Join(root, "Notebook"))
	if e != nil {
		t.Fatal(e)
	}
	defer vault.Close()
	_ = os.WriteFile(filepath.Join(vault.Path(), "AGENTS.md"), []byte("UNINHERITED_CONTEXT_SENTINEL"), 0600)
	var oldEffects, newEffects atomic.Int32
	h := &acceptanceTools{defs: fixtureDefinitions("string")}
	var s *Session
	var taskError atomic.Value
	workerA, workerB := make(chan struct{}), make(chan struct{})
	enteredA, enteredB := make(chan struct{}, 1), make(chan struct{}, 1)
	h.call = func(callCtx context.Context, name string, args json.RawMessage) api.ToolResult {
		var err error
		switch name {
		case "FixtureLookup":
			oldEffects.Add(1)
		case "FixtureDelegate":
			for _, id := range []string{"a", "b"} {
				dir := filepath.Join(root, "worker-"+id)
				_ = os.Mkdir(dir, 0700)
				_, err = s.StartWork(callCtx, api.WorkStart{ID: "task-" + id, Workspace: dir, Instructions: "Independent worker without resident skills", TaskStart: api.TaskStart{RequestID: "fixture-worker-" + id, Title: id, Prompt: "CASE_WORKER_" + strings.ToUpper(id)}})
				if err != nil {
					taskError.Store(err.Error())
					break
				}
			}
		case "FixtureSchedule":
			err = s.AuthorizeBackground(callCtx, "fixture-reminder", "original-user-schedule")
		}
		text := "FIXTURE_CALLBACK_OK"
		if err != nil {
			text = err.Error()
		}
		return api.ToolResult{IsError: err != nil, Content: []map[string]string{{"type": "text", "text": text}}}
	}
	config := &api.ToolConnection{Instructions: "APPLICATION_OLD_INSTRUCTIONS", NotebookDirectory: vault.Path(), Host: h, PrepareTurn: func(ctx context.Context) error { return vault.Refresh(ctx, time.Now()) }, FinishTurn: func() { _ = vault.Refresh(context.Background(), time.Now()) }}
	open := func() {
		s = New(Options{Directory: filepath.Join(root, "bot"), Settings: settings, Execution: api.ExecutionSettings{Model: "openai/gpt-5.4-mini", Effort: "low", ApprovalMode: "workspace-write"}})
		if e = s.ConfigureBotTools(config); e != nil {
			t.Fatal(e)
		}
		if e = s.Connect(ctx); e != nil {
			t.Fatal(e)
		}
		waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
	}
	open()
	defer func() { _ = s.Close(context.Background()) }()
	if !t.Run("B01_B02_native_notebook", func(t *testing.T) {
		prior, e := s.Configuration(ctx)
		if e != nil {
			t.Fatal(e)
		}
		model.set("CASE_NATIVE", modelStep{Name: "Write", Args: map[string]string{"path": "MEMORY.md", "content": "# Memory\nNative identity sentinel"}}, modelStep{Name: "Write", Args: map[string]string{"path": "2026/09/23/acceptance.md", "content": "Dated note sentinel"}}, modelStep{Name: "RunCommand", Args: map[string]string{"command": "/bin/cat MEMORY.md > result.txt"}})
		submitAcceptance(t, ctx, s, "CASE_NATIVE")
		for _, name := range []string{"MEMORY.md", "result.txt"} {
			b, e := os.ReadFile(filepath.Join(vault.Path(), name))
			if e != nil || !strings.Contains(string(b), "Native identity sentinel") {
				t.Fatalf("native file %s: %q %v", name, b, e)
			}
		}
		model.set("CASE_READ", modelStep{Name: "Read", Args: map[string]string{"path": "MEMORY.md"}})
		submitAcceptance(t, ctx, s, "CASE_READ")
		after, e := s.Configuration(ctx)
		if e != nil || after.Revision != prior.Revision {
			t.Fatal("Notebook writes changed configuration", e)
		}
		requests := model.seen("CASE_READ")
		raw, _ := json.Marshal(requests[len(requests)-1])
		if !strings.Contains(string(raw), "Native identity sentinel") || strings.Contains(string(raw), "UNINHERITED_CONTEXT_SENTINEL") {
			t.Fatal("native read or explicit inheritance failed")
		}
		waitAcceptance(t, ctx, func() bool {
			raw, _ := os.ReadFile(filepath.Join(vault.Path(), "INDEX.md"))
			return strings.Contains(string(raw), "acceptance.md")
		})
	}) {
		return
	}
	if !t.Run("B03_B04_B05_B06_hot_configuration", func(t *testing.T) {
		release := make(chan struct{})
		entered := make(chan struct{}, 1)
		model.set("CASE_HOT", modelStep{Name: "FixtureLookup", Args: map[string]string{"key": "old"}, Block: release, Entered: entered})
		before := s.Snapshot().CurrentTurn
		receipt, e := s.Submit(ctx, api.Submission{ID: "hot-request", Text: "CASE_HOT"}, nil)
		if e != nil || receipt.Outcome != "accepted" {
			t.Fatal(receipt, e)
		}
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		current, e := s.Configuration(ctx)
		if e != nil {
			t.Fatal(e)
		}
		h2 := &acceptanceTools{defs: fixtureDefinitions("integer"), call: func(context.Context, string, json.RawMessage) api.ToolResult {
			newEffects.Add(1)
			return api.ToolResult{Content: []map[string]string{{"type": "text", "text": "NEW_CALLBACK_OK"}}}
		}}
		next := config.Clone()
		next.Host = h2
		next.Instructions = "APPLICATION_NEW_INSTRUCTIONS"
		if e = s.ConfigureBotTools(next); e != nil {
			t.Fatal(e)
		}
		if _, e = s.UpdateConfiguration(ctx, "unsupported-priority", string(current.Revision), map[string]any{"service_tier": "priority"}); e == nil {
			close(release)
			t.Fatal("synthetic endpoint should not advertise official priority service")
		}
		var rejected *remoteError
		if !errors.As(e, &rejected) || rejected.Status != 400 || rejected.Code != "unsupported" {
			close(release)
			t.Fatalf("unsupported configuration must be an explicit HTTP 400: %v", e)
		}
		updated, e := s.UpdateConfiguration(ctx, "hot-update", string(current.Revision), map[string]any{"model": "openai/gpt-5.4", "reasoning_effort": "high", "service_tier": "", "instructions": next.Instructions, "tools_version": s.profile.ToolsVersion, "tools": s.profile.Tools})
		if e != nil {
			close(release)
			var remote *remoteError
			if errors.As(e, &remote) {
				t.Fatalf("synthetic configuration error: %s (%s)", e, remote.detail)
			}
			t.Fatal(e)
		}
		if updated.Revision == current.Revision || updated.LastRequest == nil || updated.LastRequest.Revision != current.Revision {
			close(release)
			t.Fatal("saved revision confused with active request")
		}
		close(release)
		waitTurn(t, ctx, s, before)
		used, e := s.Configuration(ctx)
		if e != nil || used.LastRequest == nil || used.LastRequest.Revision != updated.Revision || used.LastRequest.TurnId != updated.LastRequest.TurnId {
			t.Fatal("same-turn hot configuration not applied", e)
		}
		if oldEffects.Load() != 1 || newEffects.Load() != 0 {
			t.Fatal("old call rerouted to new catalog")
		}
		requests := model.seen("CASE_HOT")
		if len(requests) != 2 || requests[0]["model"] != "gpt-5.4-mini" || requests[1]["model"] != "gpt-5.4" || requests[1]["instructions"] != next.Instructions {
			t.Fatal("actual provider configuration differs")
		}
		reasoning, _ := requests[1]["reasoning"].(map[string]any)
		if reasoning["effort"] != "high" {
			t.Fatal("actual provider effort differs")
		}
		var schemaChanged bool
		if tools, ok := requests[1]["tools"].([]any); ok {
			for _, tool := range tools {
				b, _ := json.Marshal(tool)
				if strings.Contains(string(b), `"name":"FixtureLookup"`) && strings.Contains(string(b), `"type":["integer","null"]`) {
					schemaChanged = true
				}
			}
		}
		if !schemaChanged {
			t.Fatal("actual provider tool schema was not updated")
		}
		noop, e := s.UpdateConfiguration(ctx, "hot-noop", string(updated.Revision), map[string]any{"instructions": next.Instructions})
		if e != nil || noop.Revision != updated.Revision {
			t.Fatal("noop changed revision", e)
		}
		if _, e = s.UpdateConfiguration(ctx, "stale-config", string(current.Revision), map[string]any{"instructions": "stale"}); e == nil {
			t.Fatal("stale CAS accepted")
		}
		// Restore the first app catalog; this is another explicit configuration change.
		if e = s.ConfigureBotTools(config); e != nil {
			t.Fatal(e)
		}
		_, e = s.UpdateConfiguration(ctx, "restore-catalog", string(updated.Revision), map[string]any{"instructions": config.Instructions, "tools_version": s.profile.ToolsVersion, "tools": s.profile.Tools})
		if e != nil {
			t.Fatal(e)
		}
		model.set("CASE_REUSED", modelStep{Name: "FixtureLookup", Args: map[string]string{"key": "next-turn"}})
		submitAcceptance(t, ctx, s, "CASE_REUSED")
		if oldEffects.Load() != 2 {
			t.Fatal("provider call ID collapsed distinct turns")
		}
	}) {
		return
	}
	if !t.Run("B08_B09_B11_workers_background", func(t *testing.T) {
		// Bot keeps its own model while newly delegated work follows the Host.
		current, err := s.Configuration(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.UpdateConfiguration(ctx, "bot-model-before-delegation", string(current.Revision), map[string]any{"model": "openai/gpt-5.4-mini", "reasoning_effort": "low"}); err != nil {
			t.Fatal(err)
		}
		var status wire.StatusSnapshot
		if err = host.json(ctx, "GET", "/status", nil, &status, "", ""); err != nil {
			t.Fatal(err)
		}
		op := "runtime-model-before-delegation"
		var result wire.CommandResult
		if err = host.json(ctx, "POST", "/configuration/use-model", wire.UseModelRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Model: "openai/gpt-5.4", ReasoningEffort: pointer("high")}, &result, op, string(status.Configuration.Revision)); err != nil || !succeeded(result.Outcome) {
			t.Fatal("runtime model selection", result.Outcome, err)
		}
		model.set("CASE_WORKER_A", modelStep{Block: workerA, Entered: enteredA})
		model.set("CASE_WORKER_B", modelStep{Block: workerB, Entered: enteredB})
		model.set("CASE_DELEGATE", modelStep{Name: "FixtureDelegate", Args: map[string]string{}})
		submitAcceptance(t, ctx, s, "CASE_DELEGATE")
		if err := taskError.Load(); err != nil {
			t.Fatal(err)
		}
		for _, ch := range []chan struct{}{enteredA, enteredB} {
			select {
			case <-ch:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		if len(s.WorkStates()) != 2 {
			t.Fatal("missing workers")
		}
		for _, id := range []string{"task-a", "task-b"} {
			s.mu.Lock()
			sid := s.state.Workers[id].Binding.SessionId
			s.mu.Unlock()
			target, err := s.WorkTerminal(ctx, id)
			if err != nil || target.Session != sid || target.Endpoint != host.origin || target.Runtime != "caelis" {
				t.Fatal("terminal did not resolve owned native Worker", target, err)
			}
			script, err := taskterminal.Script(target)
			if err != nil || !strings.Contains(script, " attach --control-url ") || strings.Contains(script, "CASE_WORKER") {
				t.Fatal("terminal script did not attach without a prompt", err)
			}
			var state wire.SessionState
			if err := host.json(ctx, "GET", "/sessions/"+idPath(sid)+"/state", nil, &state, "", ""); err != nil || state.SessionId != sid {
				t.Fatal("user cannot attach native Worker", err)
			}
			key := "CASE_WORKER_" + strings.ToUpper(strings.TrimPrefix(id, "task-"))
			requests := model.seen(key)
			if len(requests) == 0 || requests[0]["model"] != "gpt-5.4" {
				t.Fatal("worker did not inherit Runtime model")
			}
		}
		resident, err := s.Configuration(ctx)
		if err != nil || resident.Profile.Model != "openai/gpt-5.4-mini" {
			t.Fatal("work model changed Bot", err)
		}
		if _, e = s.StopWork(ctx, "task-a"); e != nil {
			t.Fatal(e)
		}
		close(workerA)
		// Detach and reconnect the same application while B stays in flight.
		sid := s.state.Session.SessionId
		if e = s.Close(ctx); e != nil {
			t.Fatal(e)
		}
		open()
		if s.state.Session.SessionId != sid {
			t.Fatal("reconnect replaced resident session")
		}
		close(workerB)
		waitAcceptance(t, ctx, func() bool {
			a, _ := s.ReadWork(ctx, "task-a")
			b, _ := s.ReadWork(ctx, "task-b")
			return a.Status == "interrupted" && b.Status == "completed"
		})
		for _, key := range []string{"CASE_WORKER_A", "CASE_WORKER_B"} {
			raw, _ := json.Marshal(model.seen(key))
			if strings.Contains(string(raw), "APPLICATION_OLD_INSTRUCTIONS") || strings.Contains(string(raw), "MEMORY.md") {
				t.Fatal("worker inherited resident context")
			}
		}
		model.set("CASE_SCHEDULE", modelStep{Name: "FixtureSchedule", Args: map[string]string{}})
		submitAcceptance(t, ctx, s, "CASE_SCHEDULE")
		before := s.Snapshot().CurrentTurn
		in := api.Submission{ID: "wake-fixture", Text: "CASE_BACKGROUND", Scheduled: true}
		r, e := s.SubmitBackground(ctx, in, []string{"fixture-reminder"})
		if e != nil || r.Outcome != "accepted" {
			t.Fatal(r, e)
		}
		waitTurn(t, ctx, s, before)
		r, e = s.SubmitBackground(ctx, in, []string{"fixture-reminder"})
		if e != nil || r.Outcome != "accepted" || len(model.seen("CASE_BACKGROUND")) != 1 {
			t.Fatal("background duplicated", e)
		}
		for _, item := range s.Snapshot().Items {
			if item.Kind == "user" && strings.Contains(item.Text, "CASE_BACKGROUND") {
				t.Fatal("scheduled prompt exposed by canonical feed")
			}
		}
		if !s.Snapshot().Scheduled {
			t.Fatal("native feed lost scheduled origin")
		}
		model.set("CASE_BACKGROUND_SKIP", modelStep{Reply: api.SilentReminder})
		before = s.Snapshot().CurrentTurn
		skipped, e := s.SubmitBackground(ctx, api.Submission{ID: "wake-silent-fixture", Text: "CASE_BACKGROUND_SKIP", Scheduled: true}, []string{"fixture-reminder"})
		if e != nil || skipped.Outcome != "accepted" {
			t.Fatal(skipped, e)
		}
		waitTurn(t, ctx, s, before)
		quiet := s.Snapshot()
		if !quiet.Quiet {
			t.Fatal("silent final was not quiet")
		}
		for _, item := range quiet.Items {
			if item.TurnKey == quiet.CurrentTurn {
				t.Fatal("silent final appeared in transcript")
			}
		}
		if e = s.RevokeBackground(ctx, "fixture-reminder"); e != nil {
			t.Fatal(e)
		}
		r, _ = s.SubmitBackground(ctx, api.Submission{ID: "wake-revoked", Text: "CASE_REVOKED"}, []string{"fixture-reminder"})
		if r.Outcome != "rejected" || len(model.seen("CASE_REVOKED")) != 0 {
			t.Fatal("revoked grant activated")
		}
	}) {
		return
	}
	if !t.Run("B10_resources", func(t *testing.T) {
		data := []byte("resource byte sentinel\n")
		resource, e := s.UploadResource(ctx, "upload-fixture", "input.txt", "text/plain", data)
		if e != nil {
			t.Fatal(e)
		}
		// A second application on the same Host cannot read this connection's resource.
		otherToken := "app-client-" + strings.Repeat("b", 64)
		var other wire.ApplicationConnection
		if e = host.json(ctx, "POST", "/applications/register", wire.ApplicationRegistration{OperationId: "other-application", Name: "resource-scope-fixture", Credential: otherToken}, &other, "other-application", ""); e != nil {
			t.Fatal(e)
		}
		otherClient, e := newClient(d.Endpoint, otherToken)
		if e != nil {
			t.Fatal(e)
		}
		defer otherClient.http.CloseIdleConnections()
		var denied wire.ApplicationResourceContent
		e = otherClient.json(ctx, "GET", "/application/sessions/"+idPath(s.state.Session.SessionId)+"/resources/"+idPath(resource.Id)+"/content", nil, &denied, "", "")
		if !isRemoteStatus(e, 403) && !isRemoteStatus(e, 404) {
			t.Fatal("cross-application resource access was not rejected", e)
		}
		model.set("CASE_RESOURCE", modelStep{Name: "ReadResource", Args: map[string]string{"resource_id": resource.Id}}, modelStep{Name: "Read", Args: map[string]string{"path": "$RESOURCE_PATH"}}, modelStep{Name: "RunCommand", Args: map[string]string{"command": "/bin/cat $RESOURCE_PATH > artifact.txt"}}, modelStep{Name: "PublishArtifact", Args: map[string]string{"path": "artifact.txt", "name": "artifact.txt", "media_type": "text/plain"}})
		submitAcceptance(t, ctx, s, "CASE_RESOURCE")
		raw, _ := json.Marshal(model.seen("CASE_RESOURCE"))
		ids := regexp.MustCompile(`app-resource-[a-f0-9]{64}`).FindAllString(string(raw), -1)
		artifact := ""
		for _, id := range ids {
			if id != resource.Id {
				artifact = id
			}
		}
		if artifact == "" {
			t.Fatal("native artifact missing")
		}
		var link string
		for _, item := range s.Snapshot().Items {
			for _, a := range item.Artifacts {
				if a.Name == "artifact.txt" {
					link = a.ID
				}
			}
		}
		if link == "" {
			t.Fatal("published artifact did not reach Bot snapshot")
		}
		local, e := s.Artifact(link)
		if e != nil {
			t.Fatal(e)
		}
		localBytes, e := os.ReadFile(local)
		if e != nil || string(localBytes) != string(data) {
			t.Fatal("download link bytes", e)
		}
		got, descriptor, e := s.ReadResource(ctx, s.state.Session.SessionId, artifact)
		if e != nil || string(got) != string(data) || descriptor.Sha256 != digest(data) {
			t.Fatal("artifact integrity", e)
		}
	}) {
		return
	}
	if !t.Run("B07_restart", func(t *testing.T) {
		before, e := s.Configuration(ctx)
		if e != nil {
			t.Fatal(e)
		}
		sid := s.state.Session.SessionId
		items := s.Snapshot().Items
		_ = s.Close(ctx)
		stop()
		start()
		open()
		after, e := s.Configuration(ctx)
		if e != nil || after.Revision != before.Revision || s.state.Session.SessionId != sid {
			t.Fatal("restart lost configuration or binding", e)
		}
		waitAcceptance(t, ctx, func() bool { return len(s.Snapshot().Items) == len(items) })
		// An explicit runtime update reconnects this same adapter, without a Bot restart.
		stop()
		start()
		if err := s.Reconnect(ctx); err != nil {
			t.Fatal("in-process reconnect", err)
		}
		after, e = s.Configuration(ctx)
		if e != nil || after.Revision != before.Revision || s.state.Session.SessionId != sid {
			t.Fatal("reconnect replaced binding", e)
		}
		waitAcceptance(t, ctx, func() bool { return len(s.Snapshot().Items) == len(items) })
	}) {
		return
	}
	model.mu.Lock()
	failure := model.failure
	model.mu.Unlock()
	if failure != "" {
		t.Fatal(failure)
	}
	if !t.Failed() {
		t.Log("B01-B11 external Host acceptance complete; synthetic provider, real macOS native tools; no daily Store or real credentials")
	}
}
