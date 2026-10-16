package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

func TestRebindOperationsAreScopedToSessionUnderSharedPrincipal(t *testing.T) {
	var mu sync.Mutex
	receipts := map[string]string{}
	configs := map[string]wire.SessionConfiguration{}
	profile := wire.SessionProfile{Execution: "workspace-write", Instructions: "updated Bot instructions", ToolsVersion: "same-catalog"}
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/control/v1/sessions/"), "/")
		if len(parts) != 2 {
			t.Errorf("unexpected route %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		sid := parts[0]
		switch parts[1] {
		case "profile":
			writeFixture(w, wire.SessionBinding{PrincipalId: "owner", SessionId: sid, Profile: wire.SessionProfile{Execution: "workspace-write"}})
		case "reviewer-state":
			writeFixture(w, wire.SessionReviewerState{SessionId: sid, ApprovalMode: "manual", Status: "manual"})
		case "configuration":
			if r.Method == "POST" {
				op := r.Header.Get("Idempotency-Key")
				if old, exists := receipts[op]; exists && old != sid {
					w.WriteHeader(http.StatusConflict)
					writeFixture(w, wire.CommandResult{OperationId: op, Outcome: "conflicted"})
					return
				}
				receipts[op] = sid
				configs[sid] = wire.SessionConfiguration{SessionId: sid, Revision: "2", Profile: profile}
				v := configs[sid]
				writeFixture(w, wire.CommandResult{OperationId: op, Outcome: "committed", Configuration: &v})
				return
			}
			v, ok := configs[sid]
			if !ok {
				v = wire.SessionConfiguration{SessionId: sid, Revision: "1", Profile: wire.SessionProfile{Execution: "workspace-write"}}
			}
			writeFixture(w, v)
		default:
			http.NotFound(w, r)
		}
	})
	other := New(Options{Directory: filepath.Join(t.TempDir(), "other")})
	other.client, other.state.PrincipalID = s.client, "owner"
	other.state.Session.SessionId = "other"
	for _, bot := range []*Session{s, other} {
		bot.profile = profile
		if err := bot.ensureSession(t.Context(), bot.client); err != nil {
			t.Fatal("same-config Bot failed rebind", err)
		}
		if err := bot.ensureSession(t.Context(), bot.client); err != nil {
			t.Fatal("reconnect failed", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(receipts) != 2 || len(configs) != 2 {
		t.Fatalf("Sessions did not get separate receipts: %v", receipts)
	}
	for id, sid := range receipts {
		bot := s
		if sid == "other" {
			bot = other
		}
		if bot.state.Typed[id].Outcome != "committed" {
			t.Fatal("rebind did not retain its own committed receipt", sid, id)
		}
	}
}

func TestIdleMaintenanceRecoversLostModelConfigurationAndReadback(t *testing.T) {
	var modelPosts, pluginPosts, receiptReads, readbackFailures atomic.Int32
	var committed atomic.Bool
	var original atomic.Value
	firstRead := make(chan struct{}, 1)
	v := wire.SessionConfiguration{SessionId: "main", Revision: "2", Profile: wire.SessionProfile{Model: "new-model", SkillRoots: []string{}, SkillDirs: []string{}, McpServers: []wire.SessionMCPServer{}}}
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
		switch {
		case r.Method == "POST" && p == "/sessions/main/configuration":
			var req struct {
				Patch map[string]json.RawMessage `json:"patch"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			op := r.Header.Get("Idempotency-Key")
			if _, model := req.Patch["model"]; model {
				modelPosts.Add(1)
				original.Store(op)
				drop(w)
				return
			}
			pluginPosts.Add(1)
			writeFixture(w, wire.CommandResult{OperationId: op, Outcome: "committed", Configuration: &v})
		case strings.HasPrefix(p, "/operations/"):
			if p != "/operations/"+original.Load().(string) {
				t.Error("queried another receipt", p)
			}
			receiptReads.Add(1)
			if !committed.Load() {
				w.WriteHeader(http.StatusNotFound)
				select {
				case firstRead <- struct{}{}:
				default:
				}
				return
			}
			writeFixture(w, wire.CommandResult{OperationId: original.Load().(string), Outcome: "committed", Configuration: &v})
		case p == "/sessions/main/configuration":
			if !committed.Load() {
				writeFixture(w, wire.SessionConfiguration{SessionId: "main", Revision: "1", Profile: wire.SessionProfile{Model: "old-model"}})
				return
			}
			if readbackFailures.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			writeFixture(w, v)
		case p == "/initialize":
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: required})
		case p == "/sessions/main/state":
			writeFixture(w, wire.SessionState{SessionId: "main"})
		default:
			t.Errorf("unexpected maintenance request %s %s", r.Method, p)
			http.NotFound(w, r)
		}
	})
	s.streams["main"] = true // Healthy idle SSE must not hide a typed operation.
	s.state.StoreID = "store"
	s.info.Capabilities = []string{atomicCapabilities}
	s.tools = &api.ToolConnection{}
	s.profile = v.Profile
	if s.needsRefreshLocked() {
		t.Fatal("fixture is not idle")
	}
	if err := s.ChangeExecution(t.Context(), api.ExecutionSettings{Model: "new-model", ApprovalMode: "workspace-write"}, func() error { t.Error("lost response persisted preferences"); return nil }); err == nil {
		t.Fatal("lost model response reported success")
	}
	if !s.needsRefreshLocked() || !s.connected {
		t.Fatal("unknown configuration did not schedule idle maintenance")
	}
	if err := s.UpdateBotPlugins(t.Context(), plugins.Selection{}); err == nil || pluginPosts.Load() != 0 {
		t.Fatal("unknown configuration did not fence plugin update")
	}
	ctx, cancel := context.WithCancel(t.Context())
	s.wg.Add(1)
	go s.pollLoop(ctx)
	t.Cleanup(func() { cancel(); s.wg.Wait() })
	select {
	case <-firstRead:
	case <-time.After(3 * time.Second):
		t.Fatal("idle maintenance never queried the original receipt")
	}
	committed.Store(true)
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		pending := s.configurationRecoveryPendingLocked()
		connected := s.connected
		s.mu.Unlock()
		if !pending {
			if !connected {
				t.Fatal("local configuration failure disconnected healthy SSE")
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	s.wg.Wait()
	s.mu.Lock()
	pending := s.configurationRecoveryPendingLocked()
	needsRefresh := s.needsRefreshLocked()
	s.mu.Unlock()
	if pending || needsRefresh || modelPosts.Load() != 1 || receiptReads.Load() != 2 || readbackFailures.Load() < 2 {
		t.Fatalf("maintenance did not settle without redispatch: pending=%v refresh=%v modelPosts=%d reads=%d readbacks=%d", pending, needsRefresh, modelPosts.Load(), receiptReads.Load(), readbackFailures.Load())
	}
	if err := s.UpdateBotPlugins(t.Context(), plugins.Selection{}); err != nil || pluginPosts.Load() != 1 || modelPosts.Load() != 1 {
		t.Fatal("recovered configuration still blocks plugins or replays model change", err)
	}
}

func TestConfigurationRecoveryBackoffDoesNotRepeatPendingReads(t *testing.T) {
	var reads atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/operations/original-receipt" {
			t.Errorf("recovery did not preserve receipt identity: %s %s", r.Method, r.URL.Path)
		}
		reads.Add(1)
		w.WriteHeader(http.StatusNotFound)
	})
	s.streams["main"] = true
	s.state.Typed["legacy-op"] = typedRecord{ReceiptID: "original-receipt", Path: "/sessions/main/configuration", Outcome: "unknown", Body: json.RawMessage(`{}`)}
	for attempt := range 8 {
		s.configurationRetryAt = time.Time{}
		if err := s.maintainConfigurations(t.Context()); err == nil {
			t.Fatal("unavailable receipt resolved")
		}
		if reads.Load() != int32(attempt+1) || s.configurationRetryDelay != min(30*time.Second, 2*time.Second*time.Duration(1<<attempt)) {
			t.Fatal("unexpected retry schedule", reads.Load(), s.configurationRetryDelay)
		}
		if s.needsRefreshLocked() {
			t.Fatal("pending typed record bypassed backoff")
		}
		if err := s.maintainConfigurations(t.Context()); err != nil || reads.Load() != int32(attempt+1) {
			t.Fatal("maintenance retried before its deadline", err, reads.Load())
		}
	}
}
