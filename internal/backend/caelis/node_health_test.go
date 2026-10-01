package caelis

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestNodeRuntimeHealthRequiresCurrentPublicProviderMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, provider                string
		missing, known, authenticated bool
	}{
		{name: "actual omitted false", provider: "mimo", known: true, authenticated: true},
		{name: "missing current key despite authenticated alternate", provider: "mimo", missing: true, known: true},
		{name: "current provider unknown despite authenticated alternate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
				case "/initialize":
					writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("node-health-store"), InstanceId: pointer("setup-instance"), Capabilities: required})
				case "/status":
					model := map[string]any{"alias": "current-config", "name": "current-upstream"}
					if tc.provider != "" {
						model["provider"] = tc.provider
					}
					if tc.missing {
						model["missing_api_key"] = true
					}
					writeFixture(w, map[string]any{"configuration": map[string]string{"revision": "9"}, "model_status": model})
				case "/completion/slash-arguments":
					writeFixture(w, []any{map[string]string{"value": "native/current", "model_config_id": "current-config"}, map[string]any{"value": "native/alternate", "model_config_id": "alternate-config", "no_auth": false}})
				default:
					t.Errorf("node health issued native effect: %s", r.URL.Path)
					w.WriteHeader(400)
				}
			})
			out, err := InspectNodeRuntimeHealth(t.Context(), settings)
			if err != nil || !out.HealthKnown || !out.Healthy || out.AuthenticationKnown != tc.known || out.Authenticated != tc.authenticated {
				t.Fatal(out, err)
			}
			for _, call := range calls {
				if call != "GET /api/control/v1/initialize" && call != "GET /api/control/v1/status" && call != "POST /api/control/v1/completion/slash-arguments" {
					t.Fatal("unexpected native effect", call)
				}
			}
			// Existing shared discovery/auth metadata are not owned or rewritten.
			if _, err := os.Stat(filepath.Join(settings.CaelisStore, ".caelis-bot-node-owner.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("node health adopted shared store", err)
			}
			data, err := os.ReadFile(filepath.Join(settings.CaelisStore, "runtime/service/auth.token"))
			if err != nil || string(data) != "HOST_SETUP_TOKEN" {
				t.Fatal("node health changed native authority", err)
			}
			b, _ := json.Marshal(out)
			if strings.Contains(string(b), "HOST_SETUP_TOKEN") || strings.Contains(string(b), settings.CaelisStore) {
				t.Fatal("private health metadata exposed", string(b))
			}
		})
	}
}
