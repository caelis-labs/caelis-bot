package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
)

func TestNativeCaelisCatalogKeepsCurrentAuthenticationTruthfulWithoutEffects(t *testing.T) {
	for _, tc := range []struct {
		name, provider string
		missing        bool
		want           api.NodeAuthentication
	}{
		{name: "omitted native false", provider: "mimo", want: api.NodeAuthenticated},
		{name: "missing current key", provider: "mimo", missing: true, want: api.NodeAuthRequired},
		{name: "unknown provider", want: api.NodeAuthUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls = append(calls, r.Method+" "+r.URL.Path)
				mu.Unlock()
				if r.Header.Get("Authorization") != "Bearer SYNTHETIC_NODE_HEALTH_TOKEN" {
					t.Error("native authority changed")
					w.WriteHeader(401)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				var result any
				switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
				case "/initialize":
					result = wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: healthPointer("shared-store"), InstanceId: healthPointer("shared-instance"), Capabilities: []string{"shared-native-workers-v1", "turn-steering-receipts-v1", "application-runtime-v1", "application-hot-configuration-v1", "application-native-execution-v1", "application-workspace-binding-v1", "application-background-activation-v1", "application-resource-transfer-v1", "execution-configuration-v1", "application-guardian-review-v1"}}
				case "/status":
					model := map[string]any{"alias": "current-config", "name": "current-upstream"}
					if tc.provider != "" {
						model["provider"] = tc.provider
					}
					if tc.missing {
						model["missing_api_key"] = true
					}
					result = map[string]any{"configuration": map[string]string{"revision": "3"}, "model_status": model}
				case "/completion/slash-arguments":
					result = []any{map[string]string{"value": "native/current", "model_config_id": "current-config"}, map[string]any{"value": "native/alternate", "model_config_id": "alternate-config", "no_auth": false}}
				default:
					t.Errorf("catalog issued native mutation %s", r.URL.Path)
					w.WriteHeader(400)
					return
				}
				_ = json.NewEncoder(w).Encode(result)
			}))
			defer server.Close()
			store := t.TempDir()
			serviceDir := filepath.Join(store, "runtime/service")
			if err := os.MkdirAll(serviceDir, 0700); err != nil {
				t.Fatal(err)
			}
			discovery, _ := json.Marshal(map[string]string{"schema_version": "caelis.control.service-discovery/v1", "endpoint": server.URL, "instance_id": "shared-instance", "principal_id": "shared-owner"})
			if err := os.WriteFile(filepath.Join(serviceDir, "discovery.json"), discovery, 0600); err != nil {
				t.Fatal(err)
			}
			token := []byte("SYNTHETIC_NODE_HEALTH_TOKEN")
			if err := os.WriteFile(filepath.Join(serviceDir, "auth.token"), token, 0600); err != nil {
				t.Fatal(err)
			}
			configuration := &CaelisConfiguration{Settings: api.RuntimeSettings{Runtime: "caelis", CaelisStore: store}}
			s := agentFixture(t)
			s.installation = &installationFixture{status: runtimemanagement.Status{Installed: true, Version: "native-fixture"}}
			s.options.Health = func(ctx context.Context, b api.NodeBackend) (NativeHealth, error) {
				if b == api.NodeCaelis {
					return configuration.Health(ctx)
				}
				return NativeHealth{}, nil
			}
			catalog, err := s.Catalog(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			var runtimeInfo *api.NodeRuntime
			for i := range catalog.Nodes[0].Runtimes {
				v := &catalog.Nodes[0].Runtimes[i]
				if v.Backend == api.NodeCaelis {
					runtimeInfo = v
				}
			}
			if runtimeInfo == nil || runtimeInfo.Authentication != tc.want || runtimeInfo.Health != api.NodeHealthy {
				t.Fatal("catalog authentication not native current truth", runtimeInfo)
			}
			for _, role := range runtimeInfo.Roles {
				if role.Eligible {
					t.Fatal("health established owned role authority", role)
				}
			}
			mu.Lock()
			actual := append([]string(nil), calls...)
			mu.Unlock()
			for _, call := range actual {
				if call != "GET /api/control/v1/initialize" && call != "GET /api/control/v1/status" && call != "POST /api/control/v1/completion/slash-arguments" {
					t.Fatal("catalog native effect", call)
				}
			}
			if len(actual) != 4 {
				t.Fatal("unexpected readonly native request count", actual)
			}
			for name, want := range map[string][]byte{"discovery.json": discovery, "auth.token": token} {
				data, err := os.ReadFile(filepath.Join(serviceDir, name))
				if err != nil || string(data) != string(want) {
					t.Fatal("native metadata rewritten", name, err)
				}
			}
			if _, err := os.Stat(filepath.Join(store, ".caelis-bot-node-owner.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("shared host adopted", err)
			}
		})
	}
}
func healthPointer(s string) *string { return &s }
