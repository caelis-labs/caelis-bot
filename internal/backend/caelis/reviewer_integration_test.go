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

// Synthetic decisions exercise the public transport and native execution, not
// model judgment quality. No private Core imports or daily Store are involved.
func TestGuardianHostIntegration(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY for external Guardian acceptance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
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
			main.serve(w, r)
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
	t.Logf("synthetic main=%s Worker=%s Runtime Guardian=%s", mainSelector, workerSelector, reviewerSelector)
	notebook := filepath.Join(root, "Notebook")
	if err = os.Mkdir(notebook, 0700); err != nil {
		t.Fatal(err)
	}
	var effects atomic.Int32
	tools := &acceptanceTools{defs: fixtureDefinitions("string"), call: func(_ context.Context, name string, args json.RawMessage) api.ToolResult {
		effects.Add(1)
		return api.ToolResult{Content: []map[string]string{{"type": "text", "text": "REVIEWED_CALLBACK_RESULT"}}}
	}}
	var s *Session
	open := func() {
		s = New(Options{Directory: filepath.Join(root, "bot"), Settings: settings, Execution: api.ExecutionSettings{Model: mainSelector}})
		if err = s.ConfigureBotTools(&api.ToolConnection{Host: tools, NotebookDirectory: notebook}); err != nil {
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
			if r.Status == status && strings.Contains(r.Action, action) {
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
				if fact.Status != status || !strings.Contains(fact.Action, action) {
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
	if !t.Run("G01_callback_gate_and_exact_once", func(t *testing.T) {
		gate := make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(gate) }) }
		defer release()
		configureReview(`{"option_id":"allow_once"}`, gate)
		main.set("CASE_REVIEW_ALLOW", modelStep{Name: "FixtureLookup", Args: map[string]any{"key": "allowed"}})
		before := s.Snapshot().CurrentTurn
		if _, e := s.Submit(ctx, api.Submission{ID: "review-allow", Text: "CASE_REVIEW_ALLOW"}, nil); e != nil {
			t.Fatal(e)
		}
		waitAcceptance(t, ctx, func() bool { return reviewFor("inProgress", "allowed") })
		assertIdentity(t, "inProgress", "allowed")
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
		waitAcceptance(t, ctx, func() bool { return reviewFor("approved", "allowed") })
		review := assertIdentity(t, "approved", "allowed")
		if effects.Load() != 1 {
			t.Fatal("approved effect count", effects.Load())
		}
		noManual()
		s.mu.Lock()
		records := clone(s.state.Calls)
		s.mu.Unlock()
		for _, record := range records {
			if record.Call.Arguments.(map[string]any)["key"] != "allowed" {
				continue
			}
			if record.Call.ItemId != review.ItemID || record.Call.TurnId != review.TurnID || record.Call.CallId != review.ToolCallID || record.Receipt == nil {
				t.Fatal("lost exact callback identity")
			}
			path := "/sessions/" + idPath(original) + "/tool-calls/" + idPath(record.Call.Id)
			var claimed wire.SessionToolCall
			if e := s.client.json(ctx, "POST", path+"/claim", struct{}{}, &claimed, "", ""); e == nil {
				t.Fatal("duplicate claim accepted")
			}
			if e := s.client.json(ctx, "POST", path+"/result", record.Receipt, nil, "", ""); e != nil {
				t.Fatal("identical receipt retry", e)
			}
		}
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
		{"DENY", `{"option_id":"reject_once","rationale":"Not authorized by the synthetic request."}`, "denied"},
		{"FAIL", `{"option_id":"permit_forever"}`, "failed"},
	} {
		if !t.Run("G03_callback_"+tc.key, func(t *testing.T) {
			configureReview(tc.reply, nil)
			key := "CASE_REVIEW_" + tc.key
			main.set(key, modelStep{Name: "FixtureLookup", Args: map[string]any{"key": tc.key}})
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
		gate := make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(gate) }) }
		defer release()
		configureReview(`{"option_id":"allow_once"}`, gate)
		main.set("CASE_REVIEW_CANCEL", modelStep{Name: "FixtureLookup", Args: map[string]any{"key": "cancelled"}})
		if _, e := s.Submit(ctx, api.Submission{ID: "review-cancel", Text: "CASE_REVIEW_CANCEL"}, nil); e != nil {
			t.Fatal(e)
		}
		waitAcceptance(t, ctx, func() bool { return reviewFor("inProgress", "cancelled") })
		assertIdentity(t, "inProgress", "cancelled")
		if e := s.Interrupt(ctx); e != nil {
			t.Fatal(e)
		}
		release()
		waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
		noCalls()
		noManual()
		if effects.Load() != 1 {
			t.Fatal("late review resurrected cancelled effect")
		}
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
		if reviewFor("approved", "allowed") || reviewFor("denied", "DENY") {
			t.Fatal("older review appeared before its history page was requested")
		}
		noCalls()
		noManual()
		if effects.Load() != 1 {
			t.Fatal("restart repeated callback")
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
		if !reviewFor("approved", "allowed") || !reviewFor("denied", "DENY") {
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
		if reviewFor("failed", "FAIL") || reviewFor("inProgress", "cancelled") {
			t.Fatal("transient review persisted as decision")
		}
		noCalls()
		noManual()
		if effects.Load() != 1 {
			t.Fatal("restart repeated callback")
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
		config := &api.ToolConnection{Host: tools, NotebookDirectory: vault.Path(), PrepareContext: vault.PrepareContext, ConsumeContext: vault.ConsumeContext}
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
		if e := s.ConfigureBotTools(&api.ToolConnection{Host: tools, NotebookDirectory: notebook}); e != nil {
			t.Fatal(e)
		}
		current, e := s.Configuration(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.UpdateConfiguration(ctx, "reused-review-catalog", string(current.Revision), map[string]any{"tools_version": s.profile.ToolsVersion, "tools": s.profile.Tools}); e != nil {
			t.Fatal(e)
		}
		caseCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		defer func() {
			if !t.Failed() {
				return
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			for _, call := range s.state.Calls {
				if call.Call.TurnId == value(s.state.Views[original].State.Run.TurnId) {
					t.Logf("callback: item=%s state=%s", call.Call.ItemId, call.Call.State)
				}
			}
			for _, fact := range s.state.Views[original].Reviews {
				if fact.TurnID == value(s.state.Views[original].State.Run.TurnId) {
					t.Logf("review: item=%s status=%s", fact.ItemID, fact.Status)
				}
			}
			t.Logf("synthetic main requests=%d effects=%d run=%s", len(main.seen("CASE_REUSED_CALL")), effects.Load(), value(s.state.Views[original].State.Run.Status))
		}()
		configureReview(`{"option_id":"allow_once"}`, nil)
		before := effects.Load()
		main.set("CASE_REUSED_CALL", modelStep{Name: "FixtureLookup", Args: map[string]any{"key": "first-of-two"}}, modelStep{Name: "FixtureLookup", Args: map[string]any{"key": "second-of-two"}})
		submitAcceptance(t, caseCtx, s, "CASE_REUSED_CALL")
		waitAcceptance(t, caseCtx, func() bool { return reviewFor("approved", "first-of-two") && reviewFor("approved", "second-of-two") })
		first := assertIdentity(t, "approved", "first-of-two")
		second := assertIdentity(t, "approved", "second-of-two")
		if first.TurnID != second.TurnID || first.ItemID == second.ItemID || first.ToolCallID != second.ToolCallID || first.ApprovalID == second.ApprovalID || effects.Load() != before+2 {
			t.Fatal("provider call ID merged native invocations")
		}
	}) {
		return
	}

	if !t.Run("G10_worker_completion_continues_normal_loop", func(t *testing.T) {
		workerReportLoopAcceptance(t, ctx, s, main, notebook, configureReview)
	}) {
		return
	}

	if !t.Run("G07_timeout_never_dispatches", func(t *testing.T) {
		// Restore the simple reviewed callback and wait through the actual Core budget.
		before := effects.Load()
		if e := s.ConfigureBotTools(&api.ToolConnection{Host: tools, NotebookDirectory: notebook}); e != nil {
			t.Fatal(e)
		}
		current, e := s.Configuration(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.UpdateConfiguration(ctx, "timeout-review-catalog", string(current.Revision), map[string]any{"tools_version": s.profile.ToolsVersion, "tools": s.profile.Tools}); e != nil {
			t.Fatal(e)
		}
		gate := make(chan struct{})
		defer close(gate)
		configureReview(`{"option_id":"allow_once"}`, gate)
		main.set("CASE_REVIEW_TIMEOUT", modelStep{Name: "FixtureLookup", Args: map[string]any{"key": "timed-out"}})
		if _, e = s.Submit(ctx, api.Submission{ID: "review-timeout", Text: "CASE_REVIEW_TIMEOUT"}, nil); e != nil {
			t.Fatal(e)
		}
		waitAcceptance(t, ctx, func() bool { return reviewFor("timedOut", "timed-out") })
		assertIdentity(t, "timedOut", "timed-out")
		noManual()
		noCalls()
		if effects.Load() != before {
			t.Fatal("timed-out review dispatched callback")
		}
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
