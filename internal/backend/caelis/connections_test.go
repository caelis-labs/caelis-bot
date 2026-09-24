package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func waitConnection(t *testing.T, s *Connections, id, stage string) api.RuntimeFlow {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var view api.RuntimeFlow
	waitAcceptance(t, ctx, func() bool {
		f, e := s.lookup(id)
		if e != nil {
			t.Fatal(e)
		}
		view = f.snapshot()
		f.mu.Lock()
		pending := f.pending
		f.mu.Unlock()
		return view.Stage == stage && (!pending || stage == "authorization")
	})
	return view
}
func TestConnectionOAuthInputCompetitionAndNoReplay(t *testing.T) {
	var starts, inputs atomic.Int32
	delivered := make(chan struct{})
	settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer HOST_SETUP_TOKEN" {
			t.Error("wrong authority")
		}
		if runtimeFixtureHandshake(w, r) {
			return
		}
		switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
		case "/completion/slash-arguments":
			var body wire.CompletionRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			command := value(body.Command)
			v := "grok"
			if strings.HasPrefix(command, "connect-baseurl:") {
				v = "https://example.invalid/v1"
			}
			if strings.HasPrefix(command, "connect-model:") {
				v = "grok-model"
			}
			writeFixture(w, []wire.SlashArgCandidate{{Value: v, Display: &v}})
		case "/configuration/connect-model":
			starts.Add(1)
			if r.Header.Get("Accept") != "text/event-stream" || r.Header.Get("If-Match") != `"9007199254740993"` {
				t.Error("missing native auth stream/CAS")
			}
			var body wire.ConnectModelRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			op := value(body.OperationId)
			w.Header().Set("Content-Type", "text/event-stream")
			emit := func(v any) {
				raw, _ := json.Marshal(v)
				fmt.Fprintf(w, "event: model_authentication\ndata: %s\n\n", raw)
				w.(http.Flusher).Flush()
			}
			emit(map[string]any{"operation_id": op, "sequence": "1", "phase": "waiting_for_browser", "verification_url": "https://example.invalid/auth", "challenge_id": "one-use", "prompt": "Code"})
			select {
			case <-delivered:
			case <-r.Context().Done():
				return
			}
			emit(map[string]any{"operation_id": op, "sequence": "2", "phase": "finished", "result": map[string]string{"operation_id": op, "outcome": "committed", "revision": "9007199254740994", "detail": "nonblocking warning"}})
		default:
			if !strings.HasSuffix(r.URL.Path, "/auth-input") {
				t.Errorf("unexpected %s", r.URL.Path)
				return
			}
			var body wire.ModelAuthenticationInput
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.ChallengeId != "one-use" || body.Input != "private-sentinel" {
				t.Error("wrong challenge/input")
			}
			if inputs.Add(1) == 1 {
				close(delivered)
			}
			writeFixture(w, struct{}{})
		}
	})
	s := &Connections{}
	defer s.Close()
	view, err := s.Start(t.Context(), settings, api.RuntimeConnectionInput{Kind: "account", Choice: "grok"})
	if err != nil {
		t.Fatal(err)
	}
	view = waitConnection(t, s, view.ID, "models")
	_, err = s.Advance(t.Context(), api.RuntimeFlowAction{ID: view.ID, Revision: view.Revision, Action: "connect", Input: api.RuntimeFlowInput{Model: "grok-model"}})
	if err != nil {
		t.Fatal(err)
	}
	view = waitConnection(t, s, view.ID, "authorization")
	if view.Authorization == nil || !view.Authorization.CanSubmit {
		t.Fatal("missing challenge")
	}
	action := api.RuntimeFlowAction{ID: view.ID, Revision: view.Revision, Action: "submit-code", Input: api.RuntimeFlowInput{Code: "private-sentinel"}}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { _, _ = s.Advance(t.Context(), action) })
	}
	wg.Wait()
	view = waitConnection(t, s, view.ID, "complete")
	if starts.Load() != 1 || inputs.Load() != 1 {
		t.Fatal("replayed auth", starts.Load(), inputs.Load())
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "private-sentinel") || view.Authorization != nil {
		t.Fatal("secret retained in terminal view")
	}
	if err = s.Cancel(t.Context(), view.ID); err != nil {
		t.Fatal(err)
	}
	f, _ := s.lookup(view.ID)
	if f.snapshot().Stage != "complete" {
		t.Fatal("cancel rewound committed outcome")
	}
}
func TestConnectionUnknownCannotRetryAndExpiry(t *testing.T) {
	var effects atomic.Int32
	settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if runtimeFixtureHandshake(w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "slash-arguments") {
			writeFixture(w, []wire.SlashArgCandidate{{Value: "fixture"}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "connect-model") {
			effects.Add(1)
			drop(w)
			return
		}
		t.Errorf("unexpected %s", r.URL.Path)
	})
	s := &Connections{}
	defer s.Close()
	view, e := s.Start(t.Context(), settings, api.RuntimeConnectionInput{Kind: "api-key", Choice: "fixture", Model: "m", APIKey: "private-sentinel"})
	if e != nil {
		t.Fatal(e)
	}
	view = waitConnection(t, s, view.ID, "unknown")
	if _, e = s.Advance(t.Context(), api.RuntimeFlowAction{ID: view.ID, Revision: view.Revision, Action: "connect", Input: api.RuntimeFlowInput{Model: "m"}}); e == nil {
		t.Fatal("unknown effect was retryable")
	}
	_ = s.Cancel(t.Context(), view.ID)
	f, _ := s.lookup(view.ID)
	if f.snapshot().Stage != "unknown" || f.request.APIKey != "" || effects.Load() != 1 {
		t.Fatal("lost uncertainty or retained secret")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	idle := &connectionFlow{ctx: ctx, cancel: cancel, changed: make(chan struct{}), view: api.RuntimeFlow{ID: "expired", Stage: "models"}}
	s.mu.Lock()
	s.flows["expired"] = idle
	s.mu.Unlock()
	expired, e := s.Wait(t.Context(), "expired", 0)
	if e != nil || expired.Stage != "failed" {
		t.Fatalf("expired waiting flow: %+v %v", expired, e)
	}
	// Synthetic idle flow has no transport to close.
	s.mu.Lock()
	delete(s.flows, "expired")
	s.mu.Unlock()
}
func TestAgentPreparationExplicitInstallAndAuthentication(t *testing.T) {
	var prepare, auth, connect atomic.Int32
	var last wire.ACPPrepareRequest
	var mu sync.Mutex
	settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if runtimeFixtureHandshake(w, r) {
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
		switch path {
		case "/completion/slash-arguments":
			var b wire.CompletionRequest
			_ = json.NewDecoder(r.Body).Decode(&b)
			switch value(b.Command) {
			case "connect-acp-agent":
				writeFixture(w, []wire.SlashArgCandidate{{Value: "antigravity"}, {Value: "custom"}})
			case "connect-acp-launcher:antigravity":
				writeFixture(w, []wire.SlashArgCandidate{{Value: "install"}})
			case "connect-acp-install:antigravity":
				writeFixture(w, []any{map[string]any{"value": "install", "runtime_setup": map[string]any{"directory": "/tmp/test-agent", "archive_url": "https://example.invalid/agent.zip", "sha256": "verified-digest", "manual_steps": []string{"Install full runtime"}}}})
			default:
				t.Errorf("unexpected command %s", value(b.Command))
			}
		case "/agents/prepare-acp":
			var b wire.PrepareACPRequest
			_ = json.NewDecoder(r.Body).Decode(&b)
			n := prepare.Add(1)
			if n == 1 && (b.Request.Install == nil || b.Request.Install.ArchiveUrl != "https://example.invalid/agent.zip" || value(b.Request.Install.Sha256) != "verified-digest") {
				t.Error("installation did not retain native plan")
			}
			if n == 2 && (b.Request.Install != nil || value(b.Request.ParentRef) != "authenticated" || value(b.Request.ModelId) != "agent-model") {
				t.Error("model preparation must be a child without reinstallation")
			}
			mu.Lock()
			last = b.Request
			mu.Unlock()
			ref := "initial"
			if n == 2 {
				ref = "selected"
			}
			writeFixture(w, map[string]any{"operation_id": value(b.OperationId), "outcome": "committed", "resource": map[string]string{"kind": "acp_preparation", "ref": ref, "digest": "digest-" + ref}})
		case "/agents/prepare-acp-auth":
			var b wire.PrepareACPAuthenticationRequest
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b.PreparationRef != "initial" || b.PreparationDigest != "digest-initial" || b.MethodId != "oauth-personal" {
				t.Error("authentication lost prepared target")
			}
			auth.Add(1)
			writeFixture(w, map[string]any{"operation_id": value(b.OperationId), "outcome": "committed", "resource": map[string]string{"kind": "acp_preparation", "ref": "authenticated", "digest": "digest-authenticated"}})
		case "/agents/connect-acp":
			var b wire.ConnectACPRequest
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b.PreparationRef != "selected" || b.PreparationDigest != "digest-selected" {
				t.Error("connect must bind selected prepared content")
			}
			connect.Add(1)
			writeFixture(w, wire.CommandResult{OperationId: value(b.OperationId), Outcome: "committed"})
		default:
			if !strings.Contains(path, "/acp-preparations/") {
				t.Errorf("unexpected %s", path)
				return
			}
			parts := strings.Split(path, "/")
			ref := parts[len(parts)-1]
			state := "ready"
			if ref == "initial" {
				state = "needs_auth"
			}
			mu.Lock()
			request := last
			mu.Unlock()
			writeFixture(w, map[string]any{"ref": ref, "content_digest": "digest-" + ref, "state": state, "request": request, "authentication_methods": []any{map[string]string{"id": "oauth-personal", "name": "Google", "type": "agent"}, map[string]string{"id": "terminal", "type": "terminal"}}, "discovery": map[string]any{"models": []any{map[string]string{"id": "agent-model", "name": "Agent Model"}}}})
		}
	})
	s := &Connections{}
	defer s.Close()
	v, e := s.Start(t.Context(), settings, api.RuntimeConnectionInput{Kind: "agent", Choice: "antigravity"})
	if e != nil {
		t.Fatal(e)
	}
	v = waitConnection(t, s, v.ID, "installation")
	if prepare.Load() != 0 || v.Installation == nil {
		t.Fatal("catalog inspection installed runtime")
	}
	v, e = s.Advance(t.Context(), api.RuntimeFlowAction{ID: v.ID, Revision: v.Revision, Action: "install", Input: api.RuntimeFlowInput{Destination: "/tmp/test-agent"}})
	if e != nil {
		t.Fatal(e)
	}
	v = waitConnection(t, s, v.ID, "auth-method")
	if v.Methods[1].Available {
		t.Fatal("terminal auth advertised without terminal capability")
	}
	_, e = s.Advance(t.Context(), api.RuntimeFlowAction{ID: v.ID, Revision: v.Revision, Action: "authenticate", Input: api.RuntimeFlowInput{Method: "terminal"}})
	if e == nil {
		t.Fatal("terminal auth dispatched")
	}
	v, e = s.Advance(t.Context(), api.RuntimeFlowAction{ID: v.ID, Revision: v.Revision, Action: "authenticate", Input: api.RuntimeFlowInput{Method: "oauth-personal"}})
	if e != nil {
		t.Fatal(e)
	}
	v = waitConnection(t, s, v.ID, "models")
	v, e = s.Advance(t.Context(), api.RuntimeFlowAction{ID: v.ID, Revision: v.Revision, Action: "connect", Input: api.RuntimeFlowInput{Model: "agent-model"}})
	if e != nil {
		t.Fatal(e)
	}
	_ = waitConnection(t, s, v.ID, "complete")
	if prepare.Load() != 2 || auth.Load() != 1 || connect.Load() != 1 {
		t.Fatal("wrong effect counts", prepare.Load(), auth.Load(), connect.Load())
	}
}

func TestBuiltInLauncherRequiresExplicitPackageChoice(t *testing.T) {
	var launches atomic.Int32
	settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if runtimeFixtureHandshake(w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "slash-arguments") {
			var b wire.CompletionRequest
			_ = json.NewDecoder(r.Body).Decode(&b)
			if value(b.Command) == "connect-acp-agent" {
				writeFixture(w, []wire.SlashArgCandidate{{Value: "fixture-agent"}})
			} else {
				writeFixture(w, []wire.SlashArgCandidate{{Value: "npx", Display: pointer("Run package")}, {Value: "global", Display: pointer("Use installed program")}})
			}
			return
		}
		if !strings.HasSuffix(r.URL.Path, "prepare-acp") {
			t.Errorf("unexpected %s", r.URL.Path)
			return
		}
		var b wire.PrepareACPRequest
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b.Request.Launcher != "global" {
			t.Error("silently chose a package launcher")
		}
		launches.Add(1)
		writeFixture(w, wire.CommandResult{OperationId: value(b.OperationId), Outcome: "rejected"})
	})
	s := &Connections{}
	defer s.Close()
	view, err := s.Start(t.Context(), settings, api.RuntimeConnectionInput{Kind: "agent", Choice: "fixture-agent"})
	if err != nil {
		t.Fatal(err)
	}
	view = waitConnection(t, s, view.ID, "launcher")
	if launches.Load() != 0 || len(view.Launchers) != 2 {
		t.Fatal("launcher inspection executed a program")
	}
	_, err = s.Advance(t.Context(), api.RuntimeFlowAction{ID: view.ID, Revision: view.Revision, Action: "choose-launcher", Input: api.RuntimeFlowInput{Launcher: "global"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = waitConnection(t, s, view.ID, "failed")
	if launches.Load() != 1 {
		t.Fatal("launcher command replayed")
	}
}
