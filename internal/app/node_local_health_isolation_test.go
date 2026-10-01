package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// Every Store and HTTP endpoint here is synthetic. In particular, HOME points
// at a temporary default Host that must remain unobserved by inactive Nodes.
func TestNodeLocalCaelisHealthKeepsInactiveStoreIsolated(t *testing.T) {
	for _, tc := range []struct {
		name, designation string
		active, ready     bool
	}{
		{name: "fresh inactive profile"},
		{name: "inactive private slot", designation: "slot", ready: true},
		{name: "inactive explicit profile", designation: "explicit", ready: true},
		{name: "ordinary active default", active: true, ready: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", filepath.Join(dir, "home"))
			var defaultCalls, designatedCalls atomic.Int32
			installNodeHealthHost(t, filepath.Join(dir, "home", ".caelis"), &defaultCalls)
			root := filepath.Join(dir, "app")
			slot := filepath.Join(root, "nodeplane", "local", "caelis-store")
			if tc.active {
				if err := localstate.Write(filepath.Join(root, "runtime.json"), struct {
					Version int `json:"version"`
					api.RuntimeSettings
				}{1, api.RuntimeSettings{Runtime: "caelis"}}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.designation != "" {
				store := slot
				if tc.designation == "explicit" {
					store = filepath.Join(dir, "explicit-private-store")
					if err := localstate.Write(filepath.Join(root, "runtime-profiles", "caelis.json"), struct {
						Version int `json:"version"`
						api.RuntimeSettings
					}{1, api.RuntimeSettings{Runtime: "caelis", CaelisStore: store}}); err != nil {
						t.Fatal(err)
					}
				}
				installNodeHealthHost(t, store, &designatedCalls)
			}
			a, err := New(root, Host{})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			before, err := a.Backend.SetupProfile("caelis")
			if err != nil {
				t.Fatal(err)
			}
			health, err := nodeLocalHealth(t.Context(), a, api.NodeCaelis)
			if err != nil || health.HealthKnown != tc.ready || health.Healthy != tc.ready || health.AuthenticationKnown != tc.ready || health.Authenticated != tc.ready || health.WorkerEligible && !tc.active {
				t.Fatal("health did not belong to designated Node Store", health, err)
			}
			if (defaultCalls.Load() > 0) != tc.active || (designatedCalls.Load() > 0) != (tc.designation != "") {
				t.Fatal("health observed a different Host", defaultCalls.Load(), designatedCalls.Load())
			}
			after, err := a.Backend.SetupProfile("caelis")
			if err != nil || after != before || a.started || a.sourceRetired {
				t.Fatal("passive health changed ordinary Runtime profile/lifecycle", before, after, err)
			}
			if !tc.active && tc.designation != "explicit" {
				settings, err := nodeLocalCaelisSettings(a, filepath.Join(root, "nodeplane", "local"), nil)
				if err != nil || settings.CaelisStore != slot || before.CaelisStore != "" {
					t.Fatal("Node health/configuration slot differs from ordinary alternate profile", settings, before, err)
				}
			}
			if tc.designation == "" && !tc.active {
				for _, path := range []string{slot, filepath.Join(root, "runtime.json"), filepath.Join(root, "runtime-profiles", "caelis.json")} {
					if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("passive health initialized an absent Node Store/profile", err)
					}
				}
			}
		})
	}
}

func installNodeHealthHost(t *testing.T, store string, calls *atomic.Int32) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		var out any
		switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
		case "/initialize":
			out = map[string]any{"protocol_version": 1, "api_version": "v1", "envelope_version": "caelis.control.envelope/v1", "store_id": "synthetic-store", "instance_id": "synthetic-instance", "capabilities": []string{"shared-native-workers-v1", "turn-steering-receipts-v1", "application-runtime-v1", "application-hot-configuration-v1", "application-native-execution-v1", "application-workspace-binding-v1", "application-background-activation-v1", "application-resource-transfer-v1", "execution-configuration-v1", "application-guardian-review-v1"}}
		case "/status":
			out = map[string]any{"configuration": map[string]string{"revision": "1"}, "model_status": map[string]string{"alias": "synthetic-current", "name": "synthetic-model", "provider": "synthetic-provider"}}
		case "/completion/slash-arguments":
			out = []any{map[string]string{"value": "synthetic/model", "model_config_id": "synthetic-current"}}
		default:
			t.Errorf("passive health issued unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(server.Close)
	service := filepath.Join(store, "runtime", "service")
	if err := localstate.Write(filepath.Join(service, "discovery.json"), map[string]string{"schema_version": "caelis.control.service-discovery/v1", "endpoint": server.URL, "instance_id": "synthetic-instance", "principal_id": "synthetic-principal"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service, "auth.token"), []byte("SYNTHETIC_NODE_HEALTH_TOKEN"), 0600); err != nil {
		t.Fatal(err)
	}
}
