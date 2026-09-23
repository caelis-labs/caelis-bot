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
	if e = ApplySetup(ctx, r); e != nil {
		t.Fatal(e)
	}
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
	t.Log("real Host: configure before Bot binding, read model and provider catalogs; no inference request")
}
