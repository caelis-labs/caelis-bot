package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type modelDeletionEngine struct {
	*testEngine
	failSync bool
}

func (e *modelDeletionEngine) ReconcileDeletedModel(_ context.Context, _ string, persist func() error) error {
	if e.failSync {
		return errors.New("fixture local sync failure")
	}
	return persist()
}

func TestConnectionProfileBeforeActivation(t *testing.T) {
	// Preparing another Runtime must not depend on the active Bot backend.
	setup := &runtimeSetup{}
	root := t.TempDir()
	want := api.RuntimeSettings{Runtime: "caelis", CLIPath: filepath.Join(root, "caelis"), CaelisStore: filepath.Join(root, "store")}
	got, err := setup.connectionProfile(&want)
	if err != nil || got != want {
		t.Fatalf("connection profile = %+v, %v; want %+v", got, err, want)
	}
	for _, invalid := range []api.RuntimeSettings{
		{Runtime: "codex"},
		{Runtime: "caelis", CLIPath: "relative/caelis"},
		{Runtime: "caelis", CaelisStore: "relative/store"},
	} {
		if _, err := setup.connectionProfile(&invalid); err == nil {
			t.Errorf("accepted invalid setup profile %+v", invalid)
		}
	}
}

func TestStopUsingSoleModelCommitsHostBeforeClearingBotPreference(t *testing.T) {
	const model = "magpie@local/fixture-model"
	for _, failSync := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "partial-local-failure"}[failSync], func(t *testing.T) {
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer FIXTURE_HOST_TOKEN" {
					t.Error("lost shared Host authority")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/control/v1/initialize":
					_ = json.NewEncoder(w).Encode(map[string]any{"instance_id": "fixture-instance", "store_id": "fixture-store", "protocol_version": 1, "api_version": "v1", "envelope_version": "caelis.control.envelope/v1", "capabilities": []string{"shared-native-workers-v1", "turn-steering-receipts-v1", "application-runtime-v1", "application-hot-configuration-v1", "application-native-execution-v1", "application-workspace-binding-v1", "application-background-activation-v1", "application-resource-transfer-v1", "execution-configuration-v1", "application-guardian-review-v1"}})
				case "/api/control/v1/status":
					_ = json.NewEncoder(w).Encode(map[string]any{"configuration": map[string]string{"revision": "9"}})
				case "/api/control/v1/agents/binding-status":
					_ = json.NewEncoder(w).Encode(map[string]any{"targets": []any{map[string]any{"id": "profile", "display_name": "Model", "effort": map[string]any{"choices": []any{}}, "backend": map[string]any{"provider": map[string]string{"model_config_id": "cfg"}}}}, "handles": []any{}})
				case "/api/control/v1/completion/slash-arguments":
					_ = json.NewEncoder(w).Encode([]any{map[string]any{"value": model, "model_config_id": "cfg", "model_selection": map[string]any{"current": true}}})
				case "/api/control/v1/agents/disconnect-candidates":
					_ = json.NewEncoder(w).Encode(map[string]any{"revision": "9", "candidates": []any{}})
				case "/api/control/v1/configuration/delete-model":
					posts.Add(1)
					var body struct {
						Model            string `json:"model"`
						OperationID      string `json:"operation_id"`
						ExpectedRevision string `json:"expected_revision"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != model || body.ExpectedRevision != "9" || body.OperationID == "" || r.Header.Get("If-Match") != `"9"` {
						t.Errorf("lost model identity or revision: %v, %+v", err, body)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"operation_id": body.OperationID, "outcome": "committed", "revision": "10"})
				default:
					t.Errorf("unexpected Host route %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			store := t.TempDir()
			service := filepath.Join(store, "runtime", "service")
			if err := os.MkdirAll(service, 0700); err != nil {
				t.Fatal(err)
			}
			discovery, _ := json.Marshal(map[string]string{"schema_version": "caelis.control.service-discovery/v1", "endpoint": server.URL, "instance_id": "fixture-instance", "principal_id": "owner"})
			if err := os.WriteFile(filepath.Join(service, "discovery.json"), discovery, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(service, "auth.token"), []byte("FIXTURE_HOST_TOKEN"), 0600); err != nil {
				t.Fatal(err)
			}
			engine := &modelDeletionEngine{testEngine: newTestEngine(), failSync: failSync}
			app, root := fixtureApp(t, engine, Host{})
			dir, _ := providerDirectory(root, "caelis")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "execution.json")
			preference := api.ExecutionSettings{Model: model, ApprovalMode: "auto_review"}
			bytes, _ := json.Marshal(preference)
			if err := os.WriteFile(path, bytes, 0600); err != nil {
				t.Fatal(err)
			}
			app.Backend.ConfigureExecution(path, preference)
			profile := api.RuntimeSettings{Runtime: "caelis", CaelisStore: store}
			result, err := app.setup.removeModelAndStop(t.Context(), profile, api.RuntimeConfigurationChange{Action: "remove-model-and-stop", ID: model, ExpectedRevision: "9"})
			if err != nil || posts.Load() != 1 {
				t.Fatalf("Host deletion: %+v, %v; posts=%d", result, err, posts.Load())
			}
			want := "committed"
			if failSync {
				want = "unknown"
			}
			if result.Outcome != want {
				t.Fatalf("result = %+v; want %s", result, want)
			}
			stored, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var saved api.ExecutionSettings
			if err := json.Unmarshal(stored, &saved); err != nil {
				t.Fatal(err)
			}
			if failSync && saved.Model != model || !failSync && saved.Model != "" {
				t.Fatalf("preference changed at wrong transaction stage: %+v", saved)
			}
		})
	}
}
