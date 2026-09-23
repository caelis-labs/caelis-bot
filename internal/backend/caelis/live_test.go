package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/botskills"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

// Opt-in, billable acceptance. The caller explicitly configures an isolated
// Store. No credential copying or ambient model configuration occurs here.
func TestConfiguredModelIntegration(t *testing.T) {
	store, model := os.Getenv("CAELIS_BOT_LIVE_STORE"), os.Getenv("CAELIS_BOT_LIVE_MODEL")
	if store == "" || model == "" {
		t.Skip("requires explicitly configured isolated Host")
	}
	home, _ := os.UserHomeDir()
	if !filepath.IsAbs(store) || filepath.Clean(store) == filepath.Join(home, ".caelis") {
		t.Fatal("use an isolated Store")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	root, e := os.MkdirTemp("", "caelis-bot-live-evidence-")
	if e != nil {
		t.Fatal(e)
	}
	t.Log("retained private evidence directory:", root)
	evidence := map[string]any{"model": model, "store": store, "cases": map[string]string{}}
	cases := evidence["cases"].(map[string]string)
	flush := func() {
		b, _ := json.MarshalIndent(evidence, "", "  ")
		_ = os.WriteFile(filepath.Join(root, "evidence.json"), b, 0600)
	}
	record := func(name string, value any) { evidence[name] = value; flush() }
	defer func() {
		evidence["passed"] = !t.Failed()
		flush()
	}()
	run := func(name string, f func(*testing.T)) bool {
		executed, skipped := false, false
		ok := t.Run(name, func(t *testing.T) {
			executed = true
			defer func() { skipped = t.Skipped() }()
			f(t)
		})
		if !executed || skipped {
			cases[name] = "not_run"
		} else if ok {
			cases[name] = "passed"
		} else {
			cases[name] = "failed"
		}
		flush()
		t.Log(name, cases[name])
		return ok
	}
	settings := api.RuntimeSettings{Runtime: "caelis", CaelisStore: store}
	bin := os.Getenv("CAELIS_BOT_LIVE_BINARY")
	var child *exec.Cmd
	stop := func() {
		if child != nil {
			_ = child.Process.Signal(os.Interrupt)
			_ = child.Wait()
			child = nil
		}
	}
	start := func() {
		if bin == "" {
			return
		}
		privateHome := filepath.Join(root, "home")
		_ = os.MkdirAll(privateHome, 0700)
		child = exec.CommandContext(ctx, bin, "serve", "--store-dir", store, "--listen", "127.0.0.1:0")
		child.Dir = root
		child.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + privateHome, "XDG_CONFIG_HOME=" + privateHome, "TMPDIR=" + os.TempDir()}
		log, err := os.OpenFile(filepath.Join(root, "host.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		child.Stdout = log
		child.Stderr = log
		if err = child.Start(); err != nil {
			log.Close()
			t.Fatal(err)
		}
		log.Close()
		ready, c := context.WithTimeout(ctx, 20*time.Second)
		defer c()
		waitAcceptance(t, ready, func() bool {
			d, token, err := Discover(settings)
			if err != nil {
				return false
			}
			cl, err := newClient(d.Endpoint, token)
			if err != nil {
				return false
			}
			defer cl.http.CloseIdleConnections()
			_, err = initialize(ready, cl)
			return err == nil
		})
	}
	defer stop()
	start()
	host, e := setupClient(ctx, settings)
	if e != nil {
		t.Fatal(e)
	}
	defer host.http.CloseIdleConnections()
	info, e := initialize(ctx, host)
	if e != nil {
		t.Fatal(e)
	}
	record("initialize", info)
	// An explicit Fast selector can be connected using the already provisioned
	// isolated OAuth credential. No login, secret or daily Store fallback.
	fast := os.Getenv("CAELIS_BOT_LIVE_FAST_MODEL")
	if fast != "" {
		parts := strings.SplitN(fast, "/", 2)
		if len(parts) != 2 {
			t.Fatal("Fast model must include provider")
		}
		var status wire.StatusSnapshot
		if e = host.json(ctx, "GET", "/status", nil, &status, "", ""); e != nil {
			t.Fatal(e)
		}
		op := "live-connect-fast-" + filepath.Base(root)
		var result wire.CommandResult
		e = host.json(ctx, "POST", "/configuration/connect-model", wire.ConnectModelRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Config: wire.ConnectConfig{Provider: parts[0], Model: parts[1]}}, &result, op, string(status.Configuration.Revision))
		if e != nil || !succeeded(result.Outcome) {
			t.Fatalf("isolated Fast model configuration: %v (%s)", e, result.Outcome)
		}
	}
	vault, e := notebook.OpenVault(filepath.Join(root, "Notebook"))
	if e != nil {
		t.Fatal(e)
	}
	defer vault.Close()
	skill, e := botskills.Install(root)
	if e != nil {
		t.Fatal(e)
	}
	var s *Session
	var oldCalls, newCalls atomic.Int32
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce atomic.Bool
	unblock := func() {
		if releaseOnce.CompareAndSwap(false, true) {
			close(release)
		}
	}
	defer unblock()
	var delegationError atomic.Value
	textResult := func(text string, err error) api.ToolResult {
		if err != nil {
			text = err.Error()
		}
		return api.ToolResult{IsError: err != nil, Content: []map[string]string{{"type": "text", "text": text}}}
	}
	defs := func(version string) []api.ToolDefinition {
		return []api.ToolDefinition{
			{Name: "LiveCheckpoint", Description: "Call once when the user requests the hot configuration checkpoint; wait for its result.", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
			{Name: "LiveVersion", Description: "Acknowledge the current configuration by submitting its version.", InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"version":{"type":"%s"}},"required":["version"],"additionalProperties":false}`, version))},
			{Name: "LiveDelegate", Description: "Start the two isolated acceptance workers explicitly requested by the user.", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
			{Name: "LiveSchedule", Description: "Authorize the single acceptance reminder explicitly requested by the user.", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
		}
	}
	h := &acceptanceTools{defs: defs("string")}
	h.call = func(callCtx context.Context, name string, args json.RawMessage) api.ToolResult {
		switch name {
		case "LiveCheckpoint":
			oldCalls.Add(1)
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
				return textResult("Checkpoint released. Follow your CURRENT instructions.", nil)
			case <-callCtx.Done():
				return textResult("", callCtx.Err())
			}
		case "LiveSchedule":
			return textResult("REMINDER_AUTHORIZED", s.AuthorizeBackground(callCtx, "live-reminder", "explicit-user-live-reminder"))
		case "LiveDelegate":
			for _, id := range []string{"a", "b"} {
				dir := filepath.Join(root, "worker-"+id)
				prompt := "Use RunCommand once to execute exactly: printf READY > ready; IFS= read -r line < gate; printf '%s\\n' \"$line\" > worker.txt . The FIFO is a test-owned local barrier. Wait for the command to finish using native process polling if needed, then Read worker.txt and reply with its content. Do not access other directories or the network."
				_, err := s.StartWork(callCtx, api.WorkStart{ID: "live-" + id, Workspace: dir, Instructions: "Complete the explicit isolated worker request using native tools. Do not create other tasks.", TaskStart: api.TaskStart{RequestID: "live-worker-" + id, Title: "Acceptance " + id, Prompt: prompt}})
				if err != nil {
					delegationError.Store(err.Error())
					return textResult("", err)
				}
			}
			return textResult("WORKERS_STARTED", nil)
		}
		return textResult("unexpected tool", fmt.Errorf("unexpected tool %s", name))
	}
	config := &api.ToolConnection{Host: h, NotebookDirectory: vault.Path(), Instructions: "You are an isolated Caelis Bot acceptance assistant. Complete only the explicit acceptance request. Use native tools, do not claim effects without successful tool results. Do not access other directories except your application skill, or the network." + botskills.Instructions(skill), PrepareTurn: func(c context.Context) error { return vault.Refresh(c, time.Now()) }, FinishTurn: func() { _ = vault.Refresh(context.Background(), time.Now()) }}
	open := func() {
		s = New(Options{Directory: filepath.Join(root, "bot"), Settings: settings, Execution: api.ExecutionSettings{Model: model, ApprovalMode: "workspace-write"}})
		if e = s.ConfigureBotTools(config); e != nil {
			t.Fatal(e)
		}
		if e = s.Connect(ctx); e != nil {
			t.Fatal(e)
		}
		waitAcceptance(t, ctx, func() bool { return s.Snapshot().Connection == "ready" })
	}
	open()
	defer func() { _ = s.Close(context.Background()) }()
	submit := func(t *testing.T, id, prompt string) {
		t.Helper()
		c, done := context.WithTimeout(ctx, 3*time.Minute)
		defer done()
		before := s.Snapshot().CurrentTurn
		r, err := s.Submit(c, api.Submission{ID: id, Text: prompt}, nil)
		if err != nil || r.Outcome != "accepted" {
			t.Fatalf("submit rejected: %s %v", r.Outcome, err)
		}
		waitTurn(t, c, s, before)
	}
	if !run("B01_B02_B06_notebook", func(t *testing.T) {
		before, err := s.Configuration(ctx)
		if err != nil {
			t.Fatal(err)
		}
		note := time.Now().Format("2006/01/02") + "/acceptance.md"
		submit(t, "live-notebook", "Your name is Caelis Acceptance. Maintain MEMORY.md with the exact phrase LIVE_NOTEBOOK_SENTINEL and a concise identity. Write the exact phrase DATED_LIVE_SENTINEL in "+note+". Use native file tools; then read both files back. Do not edit INDEX.md. Reply only Done after success.")
		for file, sentinel := range map[string]string{"MEMORY.md": "LIVE_NOTEBOOK_SENTINEL", note: "DATED_LIVE_SENTINEL"} {
			raw, err := os.ReadFile(filepath.Join(vault.Path(), file))
			if err != nil || !strings.Contains(string(raw), sentinel) {
				t.Fatalf("missing expected native bytes: %s", file)
			}
		}
		waitAcceptance(t, ctx, func() bool {
			raw, _ := os.ReadFile(filepath.Join(vault.Path(), "INDEX.md"))
			return strings.Contains(string(raw), "acceptance.md")
		})
		submit(t, "live-recall", "Read MEMORY.md and today's acceptance.md with native Read. Reply with the two sentinel strings you read, and nothing else.")
		after, err := s.Configuration(ctx)
		if err != nil || before.Revision != after.Revision {
			t.Fatal("Notebook changed config revision", err)
		}
		record("notebook", map[string]any{"revision": after.Revision, "request": after.LastRequest, "memory_sha256": digest(mustLiveRead(t, filepath.Join(vault.Path(), "MEMORY.md")))})
	}) {
		return
	}
	if !run("B10_resources", func(t *testing.T) {
		data := []byte("LIVE_RESOURCE_SENTINEL_4a3c059\n")
		resource, err := s.UploadResource(ctx, "live-upload", "input.txt", "text/plain", data)
		if err != nil {
			t.Fatal(err)
		}
		submit(t, "live-resource", "Use ReadResource to materialize resource "+resource.Id+". Use Read on its returned path, then RunCommand to copy that file byte-for-byte into artifact.txt in your working directory. PublishArtifact with path artifact.txt, name artifact.txt and media_type text/plain. Do not add or change any bytes. Reply only Done after publication.")
		var link string
		for _, item := range s.Snapshot().Items {
			for _, a := range item.Artifacts {
				if a.Name == "artifact.txt" {
					link = a.ID
				}
			}
		}
		if link == "" {
			t.Fatal("native artifact did not reach Bot download projection")
		}
		path, err := s.Artifact(link)
		if err != nil {
			t.Fatal(err)
		}
		actual := mustLiveRead(t, path)
		if string(actual) != string(data) {
			t.Fatal("artifact bytes differ")
		}
		record("resource", map[string]any{"size": len(actual), "sha256": digest(actual), "bot_download": true})
	}) {
		return
	}
	if !run("B11_approval_reconnect", func(t *testing.T) {
		c, done := context.WithTimeout(ctx, 3*time.Minute)
		defer done()
		target := filepath.Join(root, "approved.txt")
		command := "printf LIVE_APPROVAL_COMPLETE > " + target
		before := s.Snapshot().CurrentTurn
		r, err := s.Submit(c, api.Submission{ID: "live-approval", Text: "Use RunCommand exactly once with command " + command + ", sandbox_permissions=require_escalated, and justification=Write the acceptance marker outside the Notebook workspace after one-shot approval. The command must wait for approval. Do not use file tools or change the command. After success reply Done."}, nil)
		if err != nil || r.Outcome != "accepted" {
			t.Fatal(r, err)
		}
		waitAcceptance(t, c, func() bool { return len(s.Snapshot().Approvals) == 1 })
		a := s.Snapshot().Approvals[0]
		if _, err = os.Stat(target); !os.IsNotExist(err) {
			t.Fatal("approval effect occurred before decision")
		}
		if err = s.Close(c); err != nil {
			t.Fatal(err)
		}
		open()
		waitAcceptance(t, c, func() bool { return len(s.Snapshot().Approvals) == 1 })
		restored := s.Snapshot().Approvals[0]
		if restored.ID != a.ID {
			t.Fatal("approval identity changed on reconnect")
		}
		// Only approve this test's exact marker command, never an arbitrary
		// model-selected effect or a durable allow rule.
		s.mu.Lock()
		ref, ok := s.approvalLocked(restored.ID)
		s.mu.Unlock()
		if !ok {
			t.Fatal("approval target disappeared")
		}
		var head wire.SessionState
		if err = s.client.json(c, "GET", "/sessions/"+idPath(ref.sid)+"/state", nil, &head, "", ""); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(head.Approval.Active.Permission)
		if !strings.Contains(string(raw), target) || !strings.Contains(string(raw), "LIVE_APPROVAL_COMPLETE") {
			t.Fatal("unrecognized approval effect")
		}
		var permission nativePermission
		_ = json.Unmarshal(raw, &permission)
		var input struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(permission.ToolCall.RawInput, &input) != nil || strings.TrimSpace(input.Command) != command {
			t.Fatal("approval command differs from the authorized marker command")
		}
		choice := ""
		for _, o := range permission.Options {
			if o.Kind == "allow_once" {
				choice = o.ID
			}
		}
		if choice == "" {
			t.Fatal("no one-shot approval option")
		}
		if err = s.Decide(c, api.Decision{ID: restored.ID, Choice: choice}); err != nil {
			t.Fatal(err)
		}
		waitTurn(t, c, s, before)
		if string(mustLiveRead(t, target)) != "LIVE_APPROVAL_COMPLETE" {
			t.Fatal("approval marker mismatch")
		}
		record("approval", map[string]any{"reconnected_target_preserved": true, "scope": "allow_once", "marker_sha256": digest(mustLiveRead(t, target))})
	}) {
		return
	}
	if !run("B08_B09_B11_workers_grant_recovery", func(t *testing.T) {
		c, done := context.WithTimeout(ctx, 4*time.Minute)
		defer done()
		for _, id := range []string{"a", "b"} {
			dir := filepath.Join(root, "worker-"+id)
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := exec.CommandContext(c, "/usr/bin/mkfifo", filepath.Join(dir, "gate")).Run(); err != nil {
				t.Fatal(err)
			}
		}
		submit(t, "live-delegate", "Call LiveDelegate exactly once to start my two isolated acceptance workers, then reply Workers started. Do not run their work yourself.")
		if err := delegationError.Load(); err != nil {
			t.Fatal(err)
		}
		waitAcceptance(t, c, func() bool {
			for _, id := range []string{"a", "b"} {
				raw, _ := os.ReadFile(filepath.Join(root, "worker-"+id, "ready"))
				if string(raw) != "READY" {
					return false
				}
			}
			return true
		})
		a, _ := s.ReadWork(c, "live-a")
		b, _ := s.ReadWork(c, "live-b")
		if a.Status != "working" || b.Status != "working" {
			t.Fatalf("workers not concurrently active: %s/%s", a.Status, b.Status)
		}
		if _, err := s.StopWork(c, "live-a"); err != nil {
			t.Fatal(err)
		}
		sid := s.state.Session.SessionId
		if err := s.Close(c); err != nil {
			t.Fatal(err)
		}
		open()
		if sid != s.state.Session.SessionId {
			t.Fatal("detach replaced resident")
		}
		gate := exec.CommandContext(c, "/bin/sh", "-c", "printf 'WORKER_B_COMPLETE\\n' > gate")
		gate.Dir = filepath.Join(root, "worker-b")
		if err := gate.Run(); err != nil {
			t.Fatal(err)
		}
		waitAcceptance(t, c, func() bool {
			a, _ = s.ReadWork(c, "live-a")
			b, _ = s.ReadWork(c, "live-b")
			return a.Status == "interrupted" && b.Status == "completed"
		})
		if !strings.Contains(string(mustLiveRead(t, filepath.Join(root, "worker-b/worker.txt"))), "WORKER_B_COMPLETE") {
			t.Fatal("worker result missing")
		}
		record("workers", map[string]any{"a": a.Status, "b": b.Status, "b_result_sha256": digest([]byte(b.Result)), "resident_preserved": true})
		submit(t, "live-schedule", "I authorize one test reminder. Call LiveSchedule once to save my authorization, then reply Done. Do not use an operating system scheduler.")
		before := s.Snapshot().CurrentTurn
		in := api.Submission{ID: "live-wake", Text: "This is the authorized test reminder. Reply exactly LIVE_REMINDER_COMPLETE. Do not call tools."}
		r, err := s.SubmitBackground(c, in, []string{"live-reminder"})
		if err != nil || r.Outcome != "accepted" {
			t.Fatal("background activation", r, err)
		}
		waitTurn(t, c, s, before)
		completed := s.Snapshot().CurrentTurn
		r, err = s.SubmitBackground(c, in, []string{"live-reminder"})
		if err != nil || r.Outcome != "accepted" || s.Snapshot().CurrentTurn != completed {
			t.Fatal("duplicate background activation", err)
		}
		if err = s.RevokeBackground(c, "live-reminder"); err != nil {
			t.Fatal(err)
		}
		r, _ = s.SubmitBackground(c, api.Submission{ID: "live-revoked", Text: in.Text}, []string{"live-reminder"})
		if r.Outcome != "rejected" {
			t.Fatal("revoked grant accepted")
		}
		record("background", map[string]any{"duplicate_turn_preserved": true, "revoked_rejected": true})
	}) {
		return
	}
	if !run("B03_B04_B05_B06_hot_configuration", func(t *testing.T) {
		c, done := context.WithTimeout(ctx, 3*time.Minute)
		defer done()
		defer unblock()
		before := s.Snapshot().CurrentTurn
		r, err := s.Submit(c, api.Submission{ID: "live-hot", Text: "Call LiveCheckpoint exactly once and wait for its result; afterwards follow the current system instructions. Do not call any other tool before LiveCheckpoint."}, nil)
		if err != nil || r.Outcome != "accepted" {
			t.Fatal(r, err)
		}
		select {
		case <-entered:
		case <-c.Done():
			t.Fatal("real model did not reach checkpoint")
		}
		current, err := s.Configuration(c)
		if err != nil {
			t.Fatal(err)
		}
		next := config.Clone()
		next.Instructions = "The live checkpoint has completed. Call LiveVersion exactly once with numeric version 2. Then reply HOT_CONFIGURATION_COMPLETE. No other tools."
		next.Host = &acceptanceTools{defs: defs("integer"), call: func(_ context.Context, name string, args json.RawMessage) api.ToolResult {
			if name != "LiveVersion" {
				return textResult("", fmt.Errorf("unexpected new tool"))
			}
			var v struct {
				Version int `json:"version"`
			}
			if json.Unmarshal(args, &v) != nil || v.Version != 2 {
				return textResult("", fmt.Errorf("expected new integer schema"))
			}
			newCalls.Add(1)
			return textResult("VERSION_2_CONFIRMED", nil)
		}}
		if err = s.ConfigureBotTools(next); err != nil {
			t.Fatal(err)
		}
		patch := map[string]any{"instructions": next.Instructions, "tools_version": s.profile.ToolsVersion, "tools": s.profile.Tools}
		alternate := os.Getenv("CAELIS_BOT_LIVE_ALTERNATE_MODEL")
		if alternate != "" {
			patch["model"] = alternate
		}
		if effort := os.Getenv("CAELIS_BOT_LIVE_EFFORT"); effort != "" {
			patch["reasoning_effort"] = effort
		}
		updated, err := s.UpdateConfiguration(c, "live-hot-update", string(current.Revision), patch)
		if err != nil {
			t.Fatal(err)
		}
		if updated.Revision == current.Revision || updated.LastRequest == nil || updated.LastRequest.Revision != current.Revision {
			t.Fatal("desired/actual revision mismatch")
		}
		unblock()
		waitTurn(t, c, s, before)
		used, err := s.Configuration(c)
		if err != nil || used.LastRequest == nil || used.LastRequest.Revision != updated.Revision || used.LastRequest.TurnId != updated.LastRequest.TurnId || oldCalls.Load() != 1 || newCalls.Load() != 1 {
			t.Fatal("hot configuration/callback version not observed", err)
		}
		noop, err := s.UpdateConfiguration(c, "live-noop", string(used.Revision), patch)
		if err != nil || noop.Revision != used.Revision {
			t.Fatal("noop incremented revision", err)
		}
		record("hot_configuration", map[string]any{"old_request": updated.LastRequest, "new_request": used.LastRequest, "old_effects": oldCalls.Load(), "new_effects": newCalls.Load(), "no_op_revision": noop.Revision})
		if err = s.ConfigureBotTools(config); err != nil {
			t.Fatal(err)
		}
		_, err = s.UpdateConfiguration(c, "live-restore", string(used.Revision), map[string]any{"model": model, "reasoning_effort": "", "instructions": config.Instructions, "tools_version": s.profile.ToolsVersion, "tools": s.profile.Tools})
		if err != nil {
			t.Fatal(err)
		}
	}) {
		return
	}
	run("B03_fast", func(t *testing.T) {
		if fast == "" {
			cases["fast_limit"] = "no explicitly configured Fast model"
			t.Skip("no Fast model")
		}
		models, err := s.Models(ctx)
		if err != nil {
			t.Fatal(err)
		}
		supported := false
		for _, m := range models {
			if m.Model == fast {
				for _, tier := range m.ServiceTiers {
					if tier.ID == "priority" {
						supported = true
					}
				}
			}
		}
		if !supported {
			t.Fatal("explicit model does not advertise priority")
		}
		current, err := s.Configuration(ctx)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := s.UpdateConfiguration(ctx, "live-fast", string(current.Revision), map[string]any{"model": fast, "service_tier": "priority", "reasoning_effort": "low"})
		if err != nil {
			t.Fatal(err)
		}
		submit(t, "live-fast-reply", "Reply exactly FAST_LIVE_COMPLETE. Do not call tools or read files for this one request.")
		used, err := s.Configuration(ctx)
		if err != nil || used.LastRequest == nil || used.LastRequest.Revision != updated.Revision || value(used.LastRequest.ServiceTier) != "priority" {
			t.Fatal("priority request receipt missing", err)
		}
		record("fast", map[string]any{"completed": true, "request": used.LastRequest, "limit": "Host request receipt and real provider completion; no latency or billing-tier claim"})
	})
	run("B07_host_restart", func(t *testing.T) {
		if bin == "" {
			t.Skip("Host not owned by fixture")
		}
		before, err := s.Configuration(ctx)
		if err != nil {
			t.Fatal(err)
		}
		sid := s.state.Session.SessionId
		if err = s.Close(ctx); err != nil {
			t.Fatal(err)
		}
		stop()
		start()
		open()
		after, err := s.Configuration(ctx)
		if err != nil || before.Revision != after.Revision || sid != s.state.Session.SessionId {
			t.Fatal("restart lost binding/configuration", err)
		}
		submit(t, "live-after-restart", "Read MEMORY.md with native Read and reply only the LIVE_NOTEBOOK_SENTINEL phrase you find. No other actions.")
		record("restart", map[string]any{"session_preserved": true, "revision": after.Revision, "real_model_continued": true})
	})
}
func mustLiveRead(t *testing.T, path string) []byte {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
