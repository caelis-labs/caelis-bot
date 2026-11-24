package caelis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	notebookstore "github.com/caelis-labs/caelis-bot/internal/notebook"
)

const attachmentWorkerFixture = "synthetic-worker-attachment-read"

// Synthetic decisions exercise the public transport and native execution, not
// model judgment quality. No private Core imports or daily Store are involved.
func TestGuardianHostIntegration(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY for external Guardian acceptance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 230*time.Second)
	defer cancel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := newExecutionFixture(t, root)
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(root, "store")}
	main := newAcceptanceModel()
	var mu sync.Mutex
	decision := `{"option_id":"allow_once"}`
	var block <-chan struct{}
	var reviewRequests []map[string]any
	var providerModels []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		providerModels = append(providerModels, body["model"])
		mu.Unlock()
		if body["model"] != "codex/gpt-6-luna" {
			r.Body = io.NopCloser(bytes.NewReader(raw))
			if body["model"] == "deepseek-v4-flash" && bytes.Contains(raw, []byte(attachmentWorkerFixture)) {
				main.serveCase(w, r, attachmentWorkerFixture)
			} else {
				main.serve(w, r)
			}
			return
		}
		mu.Lock()
		reply, gate := decision, block
		reviewRequests = append(reviewRequests, body)
		mu.Unlock()
		if gate != nil {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": reply}, "finish_reason": "stop"}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
	}))
	defer server.Close()
	configureReview := func(reply string, gate <-chan struct{}) { mu.Lock(); decision, block = reply, gate; mu.Unlock() }
	var cmd *exec.Cmd
	start := func() {
		cmd = exec.CommandContext(ctx, bin, "serve", "--store-dir", settings.CaelisStore, "--listen", "127.0.0.1:0")
		cmd.Dir = root
		cmd.Env = env.env
		log, e := os.OpenFile(filepath.Join(root, "host.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		cmd.Stdout, cmd.Stderr = log, log
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		_ = log.Close()
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
	d, token, err := Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	host, err := newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	defer host.http.CloseIdleConnections()
	for _, name := range []string{"mimo-v2.6-flash", "deepseek-v4-flash", "codex/gpt-6-luna"} {
		var state wire.StatusSnapshot
		if err = host.json(ctx, "GET", "/status", nil, &state, "", ""); err != nil {
			t.Fatal(err)
		}
		op := "configure-" + name
		var result wire.CommandResult
		err = host.json(ctx, "POST", "/configuration/connect-model", wire.ConnectModelRequest{OperationId: &op, ExpectedRevision: &state.Configuration.Revision, Config: wire.ConnectConfig{Provider: "custom-chat", Model: name, BaseUrl: pointer(server.URL + "/v1"), ApiKey: pointer("SYNTHETIC_ONLY")}}, &result, op, string(state.Configuration.Revision))
		if err != nil || !succeeded(result.Outcome) {
			t.Fatal("model configuration", err)
		}
	}
	mainSelector := fixtureModelSelector(t, ctx, host, "mimo-v2.6-flash")
	workerSelector := fixtureModelSelector(t, ctx, host, "deepseek-v4-flash")
	reviewerSelector := fixtureModelSelector(t, ctx, host, "codex/gpt-6-luna")
	reviewerSelector = bindFixtureGuardian(t, ctx, host, reviewerSelector, "main-guardian-binding")
	if !t.Run("G00_private_stdio_worker_round_trip", func(t *testing.T) {
		configureReview(`{"option_id":"allow_once"}`, nil)
		privateStdioWorkerRoundTrip(t, settings, mainSelector, workerSelector, main, configureReview)
	}) {
		return
	}
	configureReview(`{"option_id":"allow_once"}`, nil)
	t.Logf("synthetic main=%s Worker=%s Runtime Guardian=%s", mainSelector, workerSelector, reviewerSelector)
	notebook := filepath.Join(root, "Notebook")
	if err = os.Mkdir(notebook, 0700); err != nil {
		t.Fatal(err)
	}
	var effects atomic.Int32
	definitions := fixtureDefinitions("string")
	for _, name := range []string{"FixtureAllowed", "FixtureDENY", "FixtureFAIL", "FixtureCancelled", "FixtureFirstOfTwo", "FixtureSecondOfTwo"} {
		definition := definitions[0]
		definition.Name = name
		definitions = append(definitions, definition)
	}
	effectsByTool := map[string]int{}
	tools := &acceptanceTools{defs: definitions, call: func(_ context.Context, name string, args json.RawMessage) api.ToolResult {
		effects.Add(1)
		mu.Lock()
		effectsByTool[name]++
		mu.Unlock()
		return api.ToolResult{Content: []map[string]string{{"type": "text", "text": "REVIEWED_CALLBACK_RESULT"}}}
	}}
	fixtureConfig := fixtureMCPConfig(t, &api.ToolConnection{NotebookDirectory: notebook}, tools)
	var s *Session
	open := func() {
		s = New(Options{Directory: filepath.Join(root, "bot"), Settings: settings, Execution: api.ExecutionSettings{Model: mainSelector}})
		if err = s.ConfigureBotTools(fixtureConfig); err != nil {
			t.Fatal(err)
		}
		if err = s.Connect(ctx); err != nil {
			t.Fatal(err)
		}
		waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
	}
	open()
	defer func() { _ = s.Close(context.Background()) }()
	original := s.state.Session.SessionId
	if p := s.state.Session.Profile; p.Reviewer != nil || value(p.Permissions.ApprovalMode) != "auto-review" || value(p.Permissions.Mode) != "workspace-write" {
		t.Fatal("Guardian/sandbox not assembled")
	}
	reviewFor := func(status, action string) bool {
		for _, r := range s.Snapshot().Reviews {
			if r.Status == status && strings.Contains(strings.ToLower(r.Action), strings.ToLower(action)) {
				return true
			}
		}
		return false
	}
	identities := map[string]reviewFact{}
	assertIdentity := func(t *testing.T, status, action string) reviewFact {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, facts := range []map[string]reviewFact{s.state.Views[original].Reviews, s.state.Views[original].LiveReviews} {
			for id, fact := range facts {
				if fact.Status != status || !strings.Contains(strings.ToLower(fact.Action), strings.ToLower(action)) {
					continue
				}
				if fact.ItemID == "" || fact.ToolCallID == "" || fact.TurnID == "" || fact.ApprovalID == "" {
					t.Fatal("review lost native identity", status)
				}
				for priorID, prior := range identities {
					if priorID == id && (prior.ItemID != fact.ItemID || prior.ToolCallID != fact.ToolCallID || prior.TurnID != fact.TurnID) {
						t.Fatal("review identity changed")
					}
					if priorID != id && prior.TurnID == fact.TurnID && prior.ItemID == fact.ItemID {
						t.Fatal("distinct invocations reused item identity")
					}
				}
				identities[id] = fact
				return fact
			}
		}
		mu.Lock()
		t.Logf("provider models=%v", providerModels)
		mu.Unlock()
		t.Logf("review facts=%+v / %+v", s.state.Views[original].Reviews, s.state.Views[original].LiveReviews)
		t.Fatal("review missing", status, action)
		return reviewFact{}
	}
	noManual := func() {
		t.Helper()
		if len(s.Snapshot().Approvals) != 0 {
			t.Fatal("automatic review created manual interaction")
		}
	}
	noCalls := func() {
		t.Helper()
		var calls []wire.SessionToolCall
		if e := s.client.json(ctx, "GET", "/sessions/"+idPath(original)+"/tool-calls", nil, &calls, "", ""); e != nil {
			t.Fatal(e)
		}
		for _, c := range calls {
			if c.State == "pending" || c.State == "claimed" {
				t.Fatal("unreviewed callback dispatch")
			}
		}
	}
	if !t.Run("G01_mcp_gate_and_exact_once", func(t *testing.T) {
		gate := make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(gate) }) }
		defer release()
		configureReview(`{"option_id":"allow_once"}`, gate)
		catalogReady := make(chan struct{})
		call := fixtureMCPStep("FixtureAllowed", map[string]any{"key": "allowed"})
		call.Block = catalogReady
		main.set("CASE_REVIEW_ALLOW", call)
		before := s.Snapshot().CurrentTurn
		if _, e := s.Submit(ctx, api.Submission{ID: "review-allow", Text: "CASE_REVIEW_ALLOW"}, nil); e != nil {
			t.Fatal(e)
		}
		waitForFixtureMCP(t, ctx, s)
		close(catalogReady)
		progress, stopProgress := context.WithTimeout(ctx, 10*time.Second)
		defer stopProgress()
		defer func() {
			if t.Failed() {
				t.Logf("fixture review progress: reviews=%+v provider requests=%d last tool result=%s",
					s.Snapshot().Reviews, len(main.seen("CASE_REVIEW_ALLOW")), dtwLastToolOutput(main.seen("CASE_REVIEW_ALLOW")))
			}
		}()
		waitAcceptance(t, progress, func() bool { return reviewFor("inProgress", "FixtureAllowed") })
		assertIdentity(t, "inProgress", "FixtureAllowed")
		noManual()
		noCalls()
		if effects.Load() != 0 {
			t.Fatal("effect before review")
		}
		s.mu.Lock()
		active := clone(s.state.Views[original].State.Approval.Active)
		instance := s.state.InstanceID
		s.mu.Unlock()
		if active != nil {
			if e := s.Decide(ctx, api.Decision{ID: approvalID(instance, original, active), Choice: "allow_once"}); e == nil {
				t.Fatal("manual bypass accepted")
			}
		}
		release()
		waitTurn(t, ctx, s, before)
		waitAcceptance(t, ctx, func() bool { return reviewFor("approved", "FixtureAllowed") })
		assertIdentity(t, "approved", "FixtureAllowed")
		if effects.Load() != 1 {
			t.Fatal("approved MCP effect count", effects.Load())
		}
		noManual()
		noCalls()
	}) {
		return
	}
	// Core permits scratch writes under the platform temporary directory.
	// A temporary worktree must place this denial fixture outside that boundary.
	effectRoot := os.Getenv("CAELIS_BOT_TEST_EFFECT_DIRECTORY")
	if effectRoot == "" {
		effectRoot = "."
	}
	effectDir, err := os.MkdirTemp(effectRoot, ".guardian-effect-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(effectDir) // This test owns this synthetic, isolated directory.
	effectDir, err = filepath.Abs(effectDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Write", "RunCommand"} {
		for _, allow := range []bool{true, false} {
			label := "deny"
			want := "denied"
			reply := `{"option_id":"reject_once","rationale":"Synthetic action is not authorized."}`
			if allow {
				label = "allow"
				want = "approved"
				reply = `{"option_id":"allow_once"}`
			}
			if !t.Run("G02_"+name+"_"+label, func(t *testing.T) {
				configureReview(reply, nil)
				target := filepath.Join(effectDir, name+"-"+label+".txt")
				args := map[string]any{"path": target, "content": "ONCE\n"}
				if name == "RunCommand" {
					args = map[string]any{"command": "printf 'ONCE\\n' >> " + executionQuote(target), "sandbox_permissions": "require_escalated", "justification": "Write the synthetic fixture requested by the acceptance test", "yield_time_ms": 1000}
				}
				key := "CASE_NATIVE_" + strings.ToUpper(name) + "_" + strings.ToUpper(label)
				main.set(key, modelStep{Name: name, Args: args})
				submitAcceptance(t, ctx, s, key)
				waitAcceptance(t, ctx, func() bool { return reviewFor(want, name+"-"+label) })
				assertIdentity(t, want, name+"-"+label)
				data, e := os.ReadFile(target)
				if allow && (e != nil || string(data) != "ONCE\n") {
					t.Fatalf("approved effect: %q %v", data, e)
				}
				if !allow && !os.IsNotExist(e) {
					t.Fatal("denied native effect executed")
				}
				noManual()
			}) {
				return
			}
		}
	}
	for _, tc := range []struct{ key, reply, status string }{
		{"DENY", `{"option_id":"cancel","rationale":"Not authorized by the synthetic request."}`, "denied"},
		{"FAIL", `{"option_id":"permit_forever"}`, "failed"},
	} {
		if !t.Run("G03_mcp_"+tc.key, func(t *testing.T) {
			configureReview(tc.reply, nil)
			key := "CASE_REVIEW_" + tc.key
			main.set(key, fixtureMCPStep("Fixture"+tc.key, map[string]any{"key": tc.key}))
			submitAcceptance(t, ctx, s, key)
			waitAcceptance(t, ctx, func() bool { return reviewFor(tc.status, tc.key) })
			assertIdentity(t, tc.status, tc.key)
			noManual()
			noCalls()
			if effects.Load() != 1 {
				t.Fatal("review failure dispatched effect")
			}
		}) {
			return
		}
	}
	if !t.Run("G04_cancel_pending_review", func(t *testing.T) {
		before := effects.Load()
		gate := make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(gate) }) }
		defer release()
		configureReview(`{"option_id":"allow_once"}`, gate)
		ready := make(chan struct{})
		call := fixtureMCPStep("FixtureCancelled", map[string]any{"key": "cancelled"})
		call.Block = ready
		main.set("CASE_REVIEW_CANCEL", call)
		if receipt, err := s.Submit(ctx, api.Submission{ID: "review-cancel", Text: "CASE_REVIEW_CANCEL"}, nil); err != nil || receipt.Outcome != "accepted" {
			t.Fatal("Core did not admit the cancellation fixture turn", receipt, err)
		}
		waitForFixtureMCP(t, ctx, s)
		close(ready)
		cancelCtx, stopCancel := context.WithTimeout(ctx, 20*time.Second)
		defer stopCancel()
		waitAcceptance(t, cancelCtx, func() bool { return reviewFor("inProgress", "FixtureCancelled") })
		pending := assertIdentity(t, "inProgress", "FixtureCancelled")
		noManual()
		s.mu.Lock()
		run := s.state.Views[original].State.Run
		s.mu.Unlock()
		if value(run.TurnId) != pending.TurnID {
			t.Fatal("interrupt target differs from the approval's original turn", run, pending.TurnID)
		}
		target := wire.TurnTarget{HandleId: value(run.HandleId), RunId: value(run.RunId), TurnId: pending.TurnID}
		if err := s.Interrupt(cancelCtx); err != nil {
			t.Fatal("interrupt the original reviewed turn", err)
		}
		waitAcceptance(t, cancelCtx, func() bool { return reviewFor("failed", "FixtureCancelled") })
		settled := assertIdentity(t, "failed", "FixtureCancelled")
		if settled.ID != pending.ID || !strings.Contains(settled.Rationale, "cancelled") {
			t.Fatal("interrupt did not cancel the original native review", settled)
		}
		s.mu.Lock()
		cancelOperation := ""
		for id, operation := range s.state.Operations {
			if operation.Path != "/sessions/"+idPath(original)+"/cancel" {
				continue
			}
			body, err := json.Marshal(wire.CancelRequest{OperationId: &id, SessionId: &original, Target: target})
			if err == nil && operation.Digest == digest(body) {
				cancelOperation = id
				break
			}
		}
		s.mu.Unlock()
		var receipt wire.CommandResult
		if cancelOperation == "" {
			t.Fatal("no journal receipt for cancellation of the original turn", pending.TurnID)
		}
		if err := s.client.json(cancelCtx, "GET", "/operations/"+idPath(cancelOperation), nil, &receipt, "", ""); err != nil ||
			receipt.OperationId != cancelOperation || value(receipt.SessionId) != original || receipt.Outcome != "committed" {
			t.Fatal("original turn cancellation receipt unavailable", cancelOperation, receipt.Outcome, err)
		}
		release()
		waitAcceptance(t, cancelCtx, func() bool { return s.Snapshot().CanSend })
		noCalls()
		noManual()
		if effects.Load() != before {
			t.Fatal("late review resurrected cancelled effect")
		}
		t.Logf("native committed cancellation settled approval %s and produced zero MCP effects", pending.ApprovalID)
	}) {
		return
	}
	if !t.Run("G05_restart_replay_and_runtime_team", func(t *testing.T) {
		// The independent main model changes; Team Guardian remains Luna.
		config, e := s.Configuration(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.UpdateConfiguration(ctx, "review-hot-model", string(config.Revision), map[string]any{"model": workerSelector}); e != nil {
			t.Fatal(e)
		}
		if e = s.Close(ctx); e != nil {
			t.Fatal(e)
		}
		// Discard only Bot's derived display cache. Startup reads the most recent
		// Turn; older decided reviews return only with explicit older-page loading.
		s.mu.Lock()
		v := s.state.Views[original]
		v.Reviews, v.LiveReviews, v.Cursor, v.Seen = nil, nil, "", map[string]bool{}
		e = s.saveLocked()
		s.mu.Unlock()
		if e != nil {
			t.Fatal(e)
		}
		stop()
		start()
		open()
		if s.state.Session.SessionId != original {
			t.Fatal("reconnect replaced conversation")
		}
		readyCtx, stopReady := context.WithTimeout(ctx, 10*time.Second)
		defer stopReady()
		waitAcceptance(t, readyCtx, func() bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.state.Views[original].CommandCaughtUp
		})
		if snapshot := s.Snapshot(); !snapshot.CanSend || !snapshot.HasEarlier {
			t.Fatal("finite-window reconnect did not restore ready state and older-page boundary", snapshot.CanSend, snapshot.HasEarlier)
		}
		if reviewFor("approved", "FixtureAllowed") || reviewFor("denied", "FixtureDENY") {
			t.Fatal("older review appeared before its history page was requested")
		}
		noCalls()
		noManual()
		if effects.Load() != 1 {
			t.Fatal("restart repeated MCP call")
		}
		s.mu.Lock()
		cursor, seen, receipt, boundary := s.state.Views[original].Cursor, clone(s.state.Views[original].Seen), s.state.LastReceipt, s.state.Views[original].HistoryBefore
		s.mu.Unlock()
		// Check Core's persisted public facts separately from Bot's bounded
		// display cache. This is one read-only eight-Turn page, never a scan.
		publicReviews := map[string]wire.Envelope{}
		e = s.client.stream(ctx, "/sessions/"+idPath(original)+"/reconnect?history_turns=8&history_before="+url.QueryEscape(boundary), "", func(f frame) error {
			if f.event != "caelis.control.delivery" {
				return nil
			}
			var delivery wire.SessionFeedDelivery
			if err := json.Unmarshal(f.data, &delivery); err != nil {
				return err
			}
			for _, event := range delivery.Events {
				if event.Kind != "caelis/approval_review" || event.Delivery.Mode != wire.DeliveryModeMirror || event.ApprovalReview == nil {
					continue
				}
				if status := value(event.ApprovalReview.Status); status == "approved" || status == "denied" {
					publicReviews[reviewID(value(event.SessionId), value(event.TurnId), value(event.ApprovalRequestId))] = event
				}
			}
			if delivery.Kind == "sync" {
				return historyComplete
			}
			return nil
		})
		if !errors.Is(e, historyComplete) {
			t.Fatal("Core older page did not finish", e)
		}
		for id, prior := range identities {
			if prior.Status != "approved" && prior.Status != "denied" {
				continue
			}
			event, ok := publicReviews[id]
			if !ok || value(event.SessionId) != original || value(event.ApprovalReview.Status) != prior.Status || value(event.ApprovalReview.ItemId) != prior.ItemID || value(event.ApprovalReview.ToolCallId) != prior.ToolCallID {
				t.Fatal("Core did not retain original decided review", id)
			}
		}
		if len(publicReviews) == 0 || len(s.Snapshot().Reviews) != 0 {
			t.Fatal("public historical decisions changed Bot's recent display window")
		}
		if e = s.LoadEarlier(ctx); e != nil {
			t.Fatal("explicit older history did not load", e)
		}
		if !reviewFor("approved", "FixtureAllowed") || !reviewFor("denied", "FixtureDENY") {
			t.Fatal("loaded older page did not restore decided review facts")
		}
		s.mu.Lock()
		preserved := s.state.Views[original].Cursor == cursor && reflect.DeepEqual(s.state.Views[original].Seen, seen) && reflect.DeepEqual(s.state.LastReceipt, receipt)
		s.mu.Unlock()
		if !preserved || !s.Snapshot().CanSend {
			t.Fatal("older history changed live admission or receipt")
		}
		for _, prior := range identities {
			if prior.Status == "approved" || prior.Status == "denied" {
				assertIdentity(t, prior.Status, prior.Action)
			}
		}
		if reviewFor("failed", "FixtureFAIL") || reviewFor("inProgress", "FixtureCancelled") {
			t.Fatal("transient review persisted as decision")
		}
		noCalls()
		noManual()
		if effects.Load() != 1 {
			t.Fatal("restart repeated MCP call")
		}
		for _, name := range []string{"Write", "RunCommand"} {
			data, e := os.ReadFile(filepath.Join(effectDir, name+"-allow.txt"))
			if e != nil || string(data) != "ONCE\n" {
				t.Fatal("restart repeated native effect")
			}
		}
		var state wire.SessionReviewerState
		if e = s.client.json(ctx, "GET", "/sessions/"+idPath(original)+"/reviewer-state", nil, &state, "", ""); e != nil || state.Status != "ready" || state.Reviewer.Model != reviewerSelector {
			t.Fatal("reviewer binding changed", e)
		}
	}) {
		return
	}

	if !t.Run("G08_upgrade_does_not_dispatch_maintenance", func(t *testing.T) {
		dir := filepath.Join(root, "upgrade")
		vault, e := notebookstore.OpenVault(filepath.Join(dir, "Notebook"))
		if e != nil {
			t.Fatal(e)
		}
		defer vault.Close()
		config := fixtureConfig.Clone()
		config.NotebookDirectory, config.PrepareContext, config.ConsumeContext = vault.Path(), vault.PrepareContext, vault.ConsumeContext
		opts := Options{Directory: filepath.Join(dir, "binding"), Settings: settings, Execution: api.ExecutionSettings{Model: mainSelector}, RequireApproval: true}
		legacy := New(opts)
		if e = legacy.ConfigureBotTools(config); e != nil {
			t.Fatal(e)
		}
		if e = legacy.Connect(ctx); e != nil {
			t.Fatal(e)
		}
		waitAcceptance(t, ctx, func() bool { return legacy.Snapshot().CanSend })
		main.set("CASE_LEGACY_CONTEXT", modelStep{Reply: "Prior assignment finished. Preserve the selected project."})
		submitAcceptance(t, ctx, legacy, "CASE_LEGACY_CONTEXT")
		old := legacy.ConversationState().Session
		if e = legacy.Close(ctx); e != nil {
			t.Fatal(e)
		}

		config.RuntimeVersion = "fixture-next-version"
		opts.RequireApproval = false
		upgraded := New(opts)
		if e = upgraded.ConfigureBotTools(config); e != nil {
			t.Fatal(e)
		}
		if e = upgraded.Connect(ctx); e != nil {
			t.Fatal(e)
		}
		defer upgraded.Close(context.Background())
		waitAcceptance(t, ctx, func() bool { return upgraded.ConversationState().Observed && upgraded.Snapshot().CanSend })
		if upgraded.ConversationState().Session != old || value(upgraded.state.Session.Profile.Permissions.ApprovalMode) != "manual" {
			t.Fatal("upgrade mutated existing runtime before handoff")
		}
		resident, e := bot.NewForRuntime(filepath.Join(dir, "resident.json"), "caelis", nil)
		if e != nil {
			t.Fatal(e)
		}
		defer resident.Close()
		if e = resident.ConfigureDream(vault, ""); e != nil {
			t.Fatal(e)
		}
		resident.Start(upgraded)
		for range 3 {
			if e = resident.Tick(ctx); e != nil {
				t.Fatal(e)
			}
		}
		current := upgraded.ConversationState()
		if len(main.seen("CASE_UPGRADE")) != 0 || current.Session != old || current.RuntimeVersion == config.RuntimeVersion || current.Turn == "" {
			t.Fatal("upgrade dispatched a new maintenance Turn or replaced the bound Session", current)
		}
		main.set("CASE_UPGRADED_INPUT", modelStep{Reply: "Continuing with the retained project."})
		submitAcceptance(t, ctx, upgraded, "CASE_UPGRADED_INPUT")
		raw, _ := json.Marshal(main.seen("CASE_UPGRADED_INPUT"))
		if !strings.Contains(string(raw), "CASE_LEGACY_CONTEXT") || strings.Contains(string(raw), "caelis-dream") {
			t.Fatal("upgrade lost the old conversation or invented Dream context")
		}
		if upgraded.ConversationState().Session != old {
			t.Fatal("upgrade replaced Session after ordinary input")
		}
		if len(upgraded.Snapshot().Items) < 4 {
			t.Fatal("upgrade lost visible history")
		}
	}) {
		return
	}

	if !t.Run("G09_reused_provider_call_ids_within_one_turn", func(t *testing.T) {
		caseCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		defer func() {
			if !t.Failed() {
				return
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			for _, fact := range s.state.Views[original].Reviews {
				if fact.TurnID == value(s.state.Views[original].State.Run.TurnId) {
					t.Logf("review: item=%s status=%s", fact.ItemID, fact.Status)
				}
			}
			t.Logf("synthetic main requests=%d effects=%d run=%s", len(main.seen("CASE_REUSED_CALL")), effects.Load(), value(s.state.Views[original].State.Run.Status))
		}()
		configureReview(`{"option_id":"allow_once"}`, nil)
		before := effects.Load()
		firstStep := fixtureMCPStep("FixtureFirstOfTwo", map[string]any{"key": "first-of-two"})
		secondStep := fixtureMCPStep("FixtureSecondOfTwo", map[string]any{"key": "second-of-two"})
		firstStep.CallID, secondStep.CallID = "provider-reused-id", "provider-reused-id"
		main.set("CASE_REUSED_CALL", firstStep, secondStep)
		submitAcceptance(t, caseCtx, s, "CASE_REUSED_CALL")
		waitAcceptance(t, caseCtx, func() bool {
			return reviewFor("approved", "FixtureFirstOfTwo") && reviewFor("approved", "FixtureSecondOfTwo")
		})
		first := assertIdentity(t, "approved", "FixtureFirstOfTwo")
		second := assertIdentity(t, "approved", "FixtureSecondOfTwo")
		mu.Lock()
		firstCount, secondCount := effectsByTool["FixtureFirstOfTwo"], effectsByTool["FixtureSecondOfTwo"]
		mu.Unlock()
		if first.TurnID != second.TurnID || first.ItemID == second.ItemID || first.ToolCallID == second.ToolCallID ||
			first.ApprovalID == second.ApprovalID || effects.Load() != before+2 || firstCount != 1 || secondCount != 1 {
			t.Fatal("provider call ID merged native invocations or changed dispatch count", firstCount, secondCount)
		}
		t.Log("one native turn used distinct item, tool call and approval IDs; each MCP tool executed once")
	}) {
		return
	}

	if !t.Run("G07_timeout_never_dispatches", func(t *testing.T) {
		// Exercise the Guardian deadline on a native command. A Code Mode
		// script has a separate CPU budget and can cancel its pending review
		// before the 90-second Guardian timer expires. G04 covers MCP cancellation.
		before := effects.Load()
		gate := make(chan struct{})
		var gateOnce sync.Once
		releaseReview := func() { gateOnce.Do(func() { close(gate) }) }
		defer releaseReview()
		configureReview(`{"option_id":"allow_once"}`, gate)
		finish := make(chan struct{})
		var finishOnce sync.Once
		releaseModel := func() { finishOnce.Do(func() { close(finish) }) }
		defer releaseModel()
		const effect = "guardian-timeout.txt"
		target := filepath.Join(effectDir, effect)
		main.set("CASE_REVIEW_TIMEOUT", modelStep{Name: "RunCommand", Args: map[string]any{
			"command":             "printf 'unexpected\\n' > " + executionQuote(target),
			"sandbox_permissions": "require_escalated",
			"justification":       "Write the synthetic Guardian timeout fixture",
		}}, modelStep{Block: finish})
		started := time.Now()
		if receipt, err := s.Submit(ctx, api.Submission{ID: "review-timeout", Text: "CASE_REVIEW_TIMEOUT"}, nil); err != nil || receipt.Outcome != "accepted" {
			t.Fatal("Core did not admit the timeout fixture turn", receipt, err)
		}
		progress, stopProgress := context.WithTimeout(ctx, 12*time.Second)
		defer stopProgress()
		waitAcceptance(t, progress, func() bool { return reviewFor("inProgress", effect) })
		pending := assertIdentity(t, "inProgress", effect)
		observed := time.Now()
		timeout, stopTimeout := context.WithTimeout(ctx, 110*time.Second)
		defer stopTimeout()
		var verdict api.Review
		waitAcceptance(t, timeout, func() bool {
			noManual()
			for _, review := range s.Snapshot().Reviews {
				if review.ID == pending.ID && review.Status != "inProgress" {
					verdict = review
					return true
				}
			}
			return false
		})
		elapsed, pendingElapsed := time.Since(started), time.Since(observed)
		releaseReview()
		releaseModel()
		waitAcceptance(t, timeout, func() bool {
			noManual()
			return s.Snapshot().CanSend
		})
		if state := s.ConversationState(); state.Turn != pending.TurnID || state.Status != "completed" {
			t.Errorf("native deadline did not settle the original turn: %#v", state)
		}
		noManual()
		noCalls()
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) || effects.Load() != before {
			t.Errorf("Guardian deadline fixture produced a side effect: file stat=%v, MCP delta=%d", err, effects.Load()-before)
		}
		if verdict.Status != "timedOut" || !strings.Contains(verdict.Rationale, "timed out") {
			t.Fatalf("original approval %s ended after %s with %s: %s", pending.ApprovalID, elapsed, verdict.Status, verdict.Rationale)
		}
		out := assertIdentity(t, "timedOut", effect)
		if pending.ID != out.ID || elapsed < 90*time.Second || pendingElapsed < 85*time.Second {
			t.Fatal("Core did not spend its native deadline on the original approval", pending.ID, out.ID, elapsed)
		}
		t.Logf("native Guardian deadline settled approval %s after %.1fs with zero command or MCP effects", pending.ApprovalID, elapsed.Seconds())
	}) {
		return
	}

	if !t.Run("G10_worker_completion_continues_normal_loop", func(t *testing.T) {
		workerReportLoopAcceptance(t, ctx, s, main, notebook, configureReview)
	}) {
		return
	}

	mu.Lock()
	requests := clone(reviewRequests)
	mu.Unlock()
	// Each selected case verifies its review/effect. This check applies to the
	// resident's explicit profile, before exercising ordinary native defaults.
	for _, req := range requests {
		if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
			t.Fatal("resident Guardian received ambient query tools")
		}
	}
	if !t.Run("G11_native_worker_snapshot_before_review", func(t *testing.T) {
		// G05 restarted the Host; use the current connection, not its old URL.
		s.mu.Lock()
		currentHost := s.client
		s.mu.Unlock()
		workerApprovalSnapshotAcceptance(t, ctx, s, currentHost, main, workerSelector, reviewerSelector, effectDir, configureReview)
	}) {
		return
	}
	t.Log("Guardian selected subtests passed; see the executed subtest list")
}
