package caelis

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func setupFixture(t *testing.T, h http.HandlerFunc) api.RuntimeSettings {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	dir := t.TempDir()
	service := filepath.Join(dir, "runtime/service")
	if e := os.MkdirAll(service, 0700); e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(discovery{Schema: "caelis.control.service-discovery/v1", Endpoint: server.URL, InstanceID: "setup-instance", PrincipalID: "local-owner"})
	if e := os.WriteFile(filepath.Join(service, "discovery.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(service, "auth.token"), []byte("HOST_SETUP_TOKEN"), 0600); e != nil {
		t.Fatal(e)
	}
	return api.RuntimeSettings{Runtime: "caelis", CaelisStore: dir}
}

func TestSetupCompatibilityClassification(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		change func(*wire.ServerInfo)
		want   string
	}{
		{name: "supported", want: "ready"},
		{name: "old protocol", change: func(i *wire.ServerInfo) { i.ProtocolVersion = 0 }, want: "incompatible"},
		{name: "missing identity", change: func(i *wire.ServerInfo) { i.StoreId = nil }, want: "incompatible"},
		{name: "missing Bot capability", change: func(i *wire.ServerInfo) { i.Capabilities = required[:len(required)-1] }, want: "incompatible"},
		{name: "expired authority", status: http.StatusUnauthorized, want: "unavailable"},
		{name: "service unavailable", status: http.StatusServiceUnavailable, want: "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var catalogCalls atomic.Int32
			settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
				case "/initialize":
					if test.status != 0 {
						w.WriteHeader(test.status)
						return
					}
					info := wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("setup-instance"), Capabilities: required}
					if test.change != nil {
						test.change(&info)
					}
					writeFixture(w, info)
				case "/completion/slash-arguments":
					catalogCalls.Add(1)
					writeFixture(w, []wire.SlashArgCandidate{{Value: "fixture/model"}})
				default:
					t.Errorf("unexpected route %s", r.URL.Path)
				}
			})
			out, err := InspectSetup(t.Context(), settings)
			if err != nil || out.State != test.want {
				t.Fatalf("state = %q, err = %v; want %q", out.State, err, test.want)
			}
			if test.want == "incompatible" && (!strings.Contains(out.Message, "协议不兼容") || !strings.Contains(out.Message, "重启")) {
				t.Fatal("incompatibility must explain the required runtime/service action")
			}
			if test.want != "ready" && (catalogCalls.Load() != 0 || len(out.Models) != 0) {
				t.Fatal("failed handshake must not expose model setup or enable switching")
			}
			if test.want == "ready" && len(out.Models) != 1 {
				t.Fatal("supported Host did not make its configured model available")
			}
		})
	}
}

func TestSetupHostAuthoritySecretRedactionAndNoReplay(t *testing.T) {
	var posts atomic.Int32
	settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer HOST_SETUP_TOKEN" {
			t.Error("not using user-only Host authority")
		}
		switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
		case "/initialize":
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("setup-instance"), Capabilities: required})
		case "/status":
			writeFixture(w, map[string]any{"configuration": map[string]string{"revision": "12"}})
		case "/completion/slash-arguments":
			writeFixture(w, []wire.SlashArgCandidate{{Value: "Fixture"}})
		case "/configuration/connect-model":
			posts.Add(1)
			if r.Header.Get("If-Match") != `"12"` || r.Header.Get("Idempotency-Key") == "" {
				t.Error("mutation lost revision/identity")
			}
			var v wire.ConnectModelRequest
			if json.NewDecoder(r.Body).Decode(&v) != nil || value(v.Config.ApiKey) != "SECRET_FIXTURE" {
				t.Error("secret was not passed to Host")
			}
			drop(w)
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
		}
	})
	e := ApplySetup(t.Context(), api.SetupRequest{Settings: settings, Action: "connect-model", Provider: "Fixture", Model: "model", APIKey: "SECRET_FIXTURE"})
	if e == nil || strings.Contains(e.Error(), "SECRET_FIXTURE") || posts.Load() != 1 {
		t.Fatal("failed/redacted/no-replay contract", e, posts.Load())
	}
	_ = filepath.WalkDir(settings.CaelisStore, func(p string, d fs.DirEntry, e error) error {
		if e == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			if strings.Contains(string(b), "SECRET_FIXTURE") {
				t.Error("Bot persisted credential")
			}
		}
		return nil
	})
}
func TestNativeSetupIntegration(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("external Caelis binary required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(t.TempDir(), "store")}
	cmd := exec.CommandContext(ctx, bin, "serve", "--store-dir", settings.CaelisStore, "--listen", "127.0.0.1:0")
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "CAELIS_") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() }()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, _, e := Discover(settings); e == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("Host discovery timed out")
		case <-tick.C:
		}
	}
	v, e := InspectSetup(ctx, settings)
	if e != nil || v.State != "models" {
		t.Fatal("empty Host should allow configuration before Bot creation", v.State, e)
	}
	providers, e := SetupCatalog(ctx, api.SetupRequest{Settings: settings, Action: "providers"})
	if e != nil {
		t.Fatal(e)
	}
	provider := ""
	for _, p := range providers {
		if strings.Contains(strings.ToLower(p.Value), "chat-compatible") {
			provider = p.Value
			break
		}
	}
	if provider == "" {
		t.Fatal("native compatible provider missing")
	}
	// This fixture never sends a paid inference request; it validates configuration receipts.
	r := api.SetupRequest{Settings: settings, Action: "connect-model", Provider: provider, BaseURL: "http://127.0.0.1:1/v1", Model: "gpt-4.1", APIKey: "SYNTHETIC_SETUP_KEY"}
	connections := &Connections{}
	defer connections.Close()
	flow, e := connections.Start(ctx, settings, api.RuntimeConnectionInput{Kind: "api-key", Choice: r.Provider, BaseURL: r.BaseURL, Model: r.Model, APIKey: r.APIKey})
	if e != nil {
		t.Fatal(e)
	}
	_ = waitConnection(t, connections, flow.ID, "complete")
	v, e = InspectSetup(ctx, settings)
	if e != nil || v.State != "ready" || len(v.Models) == 0 {
		t.Fatal("configuration did not become available", v.State, e)
	}
	if _, e = SetupCatalog(ctx, api.SetupRequest{Settings: settings, Action: "endpoints", Provider: provider}); e != nil {
		t.Fatal(e)
	}
	if _, e = SetupCatalog(ctx, api.SetupRequest{Settings: settings, Action: "models", Provider: provider, BaseURL: r.BaseURL}); e != nil {
		t.Fatal(e)
	}

	read := func() api.RuntimeConfiguration {
		t.Helper()
		view, err := ReadRuntimeConfiguration(ctx, settings)
		if err != nil {
			t.Fatal(err)
		}
		return view
	}
	view := read()
	if len(view.Team.Models) == 0 || len(view.Team.Roles) == 0 {
		t.Fatal("native Team catalog is empty")
	}
	mutate := func(change api.RuntimeConfigurationChange) {
		t.Helper()
		change.ExpectedRevision = view.Revision
		result, err := ChangeRuntimeConfiguration(ctx, settings, change)
		if err != nil || result.Outcome != "committed" {
			t.Fatalf("%s: %+v %v", change.Action, result, err)
		}
		view = read()
	}
	mutate(api.RuntimeConfigurationChange{Action: "main", Selection: api.WorkExecutionSettings{Model: view.Models[0].Model}})
	stale := view.Revision
	mutate(api.RuntimeConfigurationChange{Action: "create-role", ID: "ui-research", Description: "Research fixture"})
	result, err := ChangeRuntimeConfiguration(ctx, settings, api.RuntimeConfigurationChange{Action: "save-set", Name: "stale-write", ExpectedRevision: stale})
	if err != nil || result.Outcome != "conflicted" {
		t.Fatalf("stale UI write must conflict: %+v %v", result, err)
	}
	profile := view.Team.Models[0].Model
	mutate(api.RuntimeConfigurationChange{Action: "bind", ID: "ui-research", Selection: api.WorkExecutionSettings{Model: profile, Effort: view.Team.Models[0].DefaultEffort}})
	bound := false
	for _, role := range view.Team.Roles {
		if role.ID == "ui-research" && role.Selection.Model == profile {
			bound = true
		}
	}
	if !bound {
		t.Fatal("native profile binding did not round trip")
	}
	mutate(api.RuntimeConfigurationChange{Action: "save-set", Name: "ui-team"})
	mutate(api.RuntimeConfigurationChange{Action: "reset", ID: "ui-research"})
	mutate(api.RuntimeConfigurationChange{Action: "apply-set", Name: "ui-team"})
	mutate(api.RuntimeConfigurationChange{Action: "delete-set", Name: "ui-team"})
	mutate(api.RuntimeConfigurationChange{Action: "delete-role", ID: "ui-research"})
	for _, kind := range []string{"account", "agent"} {
		catalog, err := ConnectionCatalog(ctx, settings, kind)
		if err != nil || len(catalog.Choices) == 0 {
			t.Fatalf("native %s catalog: %+v %v", kind, catalog, err)
		}
	}

	peer, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	flow, err = connections.Start(ctx, settings, api.RuntimeConnectionInput{Kind: "agent", Choice: "custom", Command: strconv.Quote(peer) + " -test.run=^TestRuntimeACPProcess$ -- runtime-acp-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	flow = waitConnection(t, connections, flow.ID, "models")
	if len(flow.Models) != 3 {
		t.Fatalf("ACP models not discovered: %+v", flow)
	}
	flow, err = connections.Advance(ctx, api.RuntimeFlowAction{ID: flow.ID, Revision: flow.Revision, Action: "connect", Input: api.RuntimeFlowInput{Model: "fixture-two"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = waitConnection(t, connections, flow.ID, "complete")
	view = read()
	agentID := ""
	for _, group := range view.Connections {
		if group.Kind == "agent" {
			agentID = group.ID
			if len(group.Models) == 0 {
				t.Fatal("ACP connection has no models")
			}
		}
	}
	if agentID == "" {
		t.Fatal("native ACP connection was misclassified as provider")
	}
	for _, group := range view.Connections {
		if group.ID == agentID && group.Kind == "agent" {
			for _, role := range view.Team.Roles {
				if role.ID == "guardian" {
					for _, allowed := range role.ModelIDs {
						for _, model := range group.Models {
							if allowed == model.ID {
								t.Fatal("native Guardian eligibility allowed an ACP profile")
							}
						}
					}
				}
			}
		}
	}
	mutate(api.RuntimeConfigurationChange{Action: "disconnect-agent", ID: agentID})
	for _, group := range view.Connections {
		if group.Kind == "agent" && group.ID == agentID {
			t.Fatal("ACP disconnect did not round trip")
		}
	}
	t.Log("real Host: asynchronous API-key connect, main model, Team profile binding, role and preset lifecycle, CAS conflict; no inference request")
}
