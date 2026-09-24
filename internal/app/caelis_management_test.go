package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

func TestCaelisUpdateRequiresLiveCompatibilityAndRefusesBusyHost(t *testing.T) {
	for _, mode := range []string{"ready", "busy", "becomes-busy", "incompatible", "start-failed"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			calls := filepath.Join(dir, "calls")
			marker := filepath.Join(dir, "started")
			t.Setenv("BOT_UPGRADE_CALLS", calls)
			t.Setenv("BOT_UPGRADE_MARKER", marker)
			t.Setenv("BOT_UPGRADE_MODE", mode)
			var inspections atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("unexpected mutation", r.URL.Path)
					w.WriteHeader(500)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/status") {
					n := inspections.Add(1)
					busy := mode == "busy" || mode == "becomes-busy" && n > 1
					_ = json.NewEncoder(w).Encode(map[string]any{"runtime": map[string]any{"running": busy}})
					return
				}
				caps := []string{}
				if _, err := os.Stat(marker); err == nil && mode != "incompatible" {
					caps = []string{"shared-native-workers-v1", "turn-steering-receipts-v1", "application-runtime-v1", "application-hot-configuration-v1", "application-native-execution-v1", "application-workspace-binding-v1", "application-background-activation-v1", "application-resource-transfer-v1"}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"instance_id": "fixture-instance", "store_id": "fixture-store", "protocol_version": 1, "api_version": "v1", "envelope_version": "caelis.control.envelope/v1", "capabilities": caps})
			}))
			defer server.Close()
			service := filepath.Join(dir, "runtime/service")
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
			binary := filepath.Join(dir, "caelis")
			script := `#!/bin/sh
printf '%s\n' "$1 $2" >> "$BOT_UPGRADE_CALLS"
case "$1" in
version) printf '{"version":"v0.62.0"}\n';;
update) printf 'caelis is up to date (v0.62.0)\n';;
service) [ "$BOT_UPGRADE_MODE" != start-failed ] || exit 1; touch "$BOT_UPGRADE_MARKER"; printf '{"state":"running"}\n';;
*) exit 1;;
esac
`
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			a, _ := fixtureApp(t, newTestEngine(), Host{})
			defer a.Close()
			result, err := a.manageCaelis(t.Context(), "update", api.RuntimeSettings{Runtime: "caelis", CLIPath: binary, CaelisStore: dir})
			if (err == nil) != (mode == "ready") {
				t.Fatal(mode, result, err)
			}
			raw, _ := os.ReadFile(calls)
			if mode == "busy" && len(raw) > 0 {
				t.Fatal("busy Host ran installer", string(raw))
			}
			if mode == "becomes-busy" && strings.Contains(string(raw), "service") {
				t.Fatal("replaced Host after new work arrived")
			}
			if mode == "ready" && strings.Count(string(raw), "service start") != 1 {
				t.Fatal("did not select installed Host once", string(raw))
			}
			if err := a.PrepareUpdate(); err != nil {
				t.Fatal("failed operation left admission frozen", err)
			}
			a.CancelUpdate()
		})
	}
}

// Opt-in, actual release executables and private Store; no account or model call.
func TestCaelisReleasedServiceUpgrade(t *testing.T) {
	old, newBinary := os.Getenv("CAELIS_BOT_TEST_PREVIOUS_BINARY"), os.Getenv("CAELIS_BOT_TEST_BINARY")
	if old == "" || newBinary == "" {
		t.Skip("set two release binaries for isolated lifecycle acceptance")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	store := filepath.Join(dir, "store")
	if _, err := caelisruntime.Manage(t.Context(), "start", old, store); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cmd := exec.Command(newBinary, "service", "stop", "--store-dir", store, "--format", "json")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("isolated service cleanup: %v %s", err, out)
		}
	}()
	sentinel := filepath.Join(store, "upgrade-sentinel")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	a, _ := fixtureApp(t, newTestEngine(), Host{})
	defer a.Close()
	v, err := a.manageCaelis(t.Context(), "apply-update", api.RuntimeSettings{Runtime: "caelis", CLIPath: newBinary, CaelisStore: store})
	if err != nil || (!strings.Contains(v.Message, "验证通过") && !strings.Contains(v.Message, "verified")) {
		t.Fatal(v, err)
	}
	raw, err := os.ReadFile(filepath.Join(store, "runtime/service/discovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	var service struct {
		Version string `json:"distribution_version"`
	}
	if json.Unmarshal(raw, &service) != nil || service.Version != v.Version {
		t.Fatal("installed and running versions differ")
	}
	if raw, err := os.ReadFile(sentinel); err != nil || string(raw) != "preserve" {
		t.Fatal("Store replaced", err)
	}
	t.Log("released older Host replaced through native lifecycle; Store retained and Bot protocol verified")
}
