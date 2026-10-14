package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// The combined onboarding/scopes path must recheck native chat eligibility on
// activation, including after an earlier setup attempt failed and was refreshed.
func TestPR164UnknownChatRefreshCannotActivateUntilQualified(t *testing.T) {
	var qualified atomic.Bool
	var failedWrites atomic.Int32
	capabilities := []string{
		"shared-native-workers-v1", "turn-steering-receipts-v1", "application-runtime-v1",
		"application-hot-configuration-v1", "application-native-execution-v1",
		"application-workspace-binding-v1", "application-background-activation-v1",
		"application-resource-transfer-v1", "execution-configuration-v1",
		"application-guardian-review-v1",
	}
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
		case "/initialize":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"protocol_version": 1, "api_version": "v1", "envelope_version": "caelis.control.envelope/v1",
				"store_id": "fixture-store", "instance_id": "fixture-instance", "capabilities": capabilities,
			})
		case "/completion/slash-arguments":
			eligibility := "unknown"
			if qualified.Load() {
				eligibility = "chat"
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"value": "fixture/model", "chat_eligibility": eligibility, "model_metadata_complete": true}})
		default:
			failedWrites.Add(1)
			http.Error(w, "synthetic setup write failed", http.StatusServiceUnavailable)
		}
	}))
	defer host.Close()

	a, root, _ := newSwitchFixture(t, true, nil)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(root, "fixture-caelis")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf '%s\\n' '{\"version\":\"0.70.0\"}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(root, "fixture-store")
	service := filepath.Join(store, "runtime", "service")
	if err := os.MkdirAll(service, 0700); err != nil {
		t.Fatal(err)
	}
	discovery, err := json.Marshal(map[string]string{
		"schema_version": "caelis.control.service-discovery/v1", "endpoint": host.URL,
		"instance_id": "fixture-instance", "principal_id": "fixture-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service, "discovery.json"), discovery, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service, "auth.token"), []byte("SYNTHETIC_ONLY"), 0600); err != nil {
		t.Fatal(err)
	}
	profile := api.RuntimeSettings{Runtime: "caelis", CLIPath: cli, CaelisStore: store}
	if _, err := a.setup.Apply(t.Context(), api.SetupRequest{Settings: profile, Action: "use-model", Model: "fixture/model"}); err == nil || failedWrites.Load() == 0 {
		t.Fatal("synthetic setup failure did not reach the Host")
	}
	for _, step := range []string{"failed setup read", "refresh and retry"} {
		state, err := a.setup.Inspect(t.Context(), profile)
		if err != nil || state.State != "models" {
			t.Fatalf("%s: Inspect = %s, %v", step, state.State, err)
		}
		if err := a.setup.Activate(t.Context(), profile); err == nil {
			t.Fatalf("%s: unknown chat model activated", step)
		}
		if a.Backend.ProviderInfo().ID != "codex" {
			t.Fatalf("%s: failed activation changed current Bot", step)
		}
	}
	qualified.Store(true)
	state, err := a.setup.Inspect(t.Context(), profile)
	if err != nil || state.State != "ready" {
		t.Fatalf("qualified model: Inspect = %s, %v", state.State, err)
	}
	if err := a.setup.Activate(t.Context(), profile); err != nil || a.Backend.ProviderInfo().ID != "caelis" {
		t.Fatalf("qualified model did not activate: %v", err)
	}
}
