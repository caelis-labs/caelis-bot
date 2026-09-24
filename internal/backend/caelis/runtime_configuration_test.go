package caelis

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func runtimeFixtureHandshake(w http.ResponseWriter, r *http.Request) bool {
	switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
	case "/initialize":
		writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("setup-instance"), Capabilities: append(append([]string{}, required...), "model-auth-stream-v1")})
	case "/status":
		writeFixture(w, map[string]any{"configuration": map[string]string{"revision": "9007199254740993"}})
	default:
		return false
	}
	return true
}
func TestRuntimeConfigurationSelectorsAndRevision(t *testing.T) {
	var status wire.AgentBindingStatus
	raw := `{"targets":[{"id":"profile-provider","display_name":"Provider model","effort":{"choices":[]},"backend":{"provider":{"model_config_id":"stored-model"}}},{"id":"acp-profile","display_name":"Agent model","effort":{"choices":[]},"backend":{"acp":{"agent_id":"agent-native"}}}],"handles":[{"eligible_profile_ids":["profile-provider"],"definition":{"handle":"orbit","configurable":true},"binding":{"profile_id":"profile-provider","speed":"fast"}},{"definition":{"handle":"self","configurable":true},"binding":{"profile_id":"profile-provider"}},{"definition":{"handle":"memory","class":"system","configurable":true},"binding":{}}]}`
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatal(err)
	}
	settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer HOST_SETUP_TOKEN" {
			t.Error("settings lost user Host authority")
		}
		if runtimeFixtureHandshake(w, r) {
			return
		}
		switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
		case "/agents/binding-status":
			if r.URL.Query().Get("include") != "eligible_profile_ids" {
				t.Error("settings must explicitly request the eligibility extension")
			}
			writeFixture(w, status)
		case "/completion/slash-arguments":
			writeFixture(w, []wire.SlashArgCandidate{{Value: "provider/vendor/model", ModelConfigId: pointer("stored-model"), ModelSelection: &wire.ModelSelection{Current: pointer(true), Effort: "high", Fast: pointer(true)}}, {Value: "acp-profile"}})
		case "/agents/disconnect-candidates":
			writeFixture(w, map[string]any{"revision": "9007199254740993", "candidates": []any{map[string]string{"agent_id": "agent-native", "name": "Native Agent"}}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	view, err := ReadRuntimeConfiguration(t.Context(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if view.Revision != "9007199254740993" || view.Main.Model != "provider/vendor/model" || view.Main.ServiceTier != "priority" || !view.OAuthAvailable {
		t.Fatalf("lost native revision/selection: %+v", view)
	}
	if len(view.Team.Roles) != 2 || len(view.Team.Roles[0].ModelIDs) != 1 || view.Team.Roles[0].ModelIDs[0] != "profile-provider" || view.Team.Roles[0].Selection.Model != "profile-provider" || view.Team.Roles[0].Selection.ServiceTier != "fast" || !view.Team.Roles[1].Inherited {
		t.Fatalf("team must retain native profile IDs, no writable self: %+v", view.Team)
	}
	if len(view.Connections) != 2 || view.Connections[1].ID != "agent-native" || view.Connections[1].Kind != "agent" || len(view.Connections[0].Models[0].Uses) != 2 {
		t.Fatalf("incorrect connection ownership: %+v", view.Connections)
	}
}
func TestRuntimeMutationCASAndAmbiguousEffects(t *testing.T) {
	for _, outcome := range []string{"committed", "conflicted", "drop", "wrong-receipt"} {
		t.Run(outcome, func(t *testing.T) {
			var posts atomic.Int32
			settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if runtimeFixtureHandshake(w, r) {
					return
				}
				if r.URL.Path != "/api/control/v1/agents/bind" {
					t.Errorf("unexpected %s", r.URL.Path)
					return
				}
				posts.Add(1)
				var body wire.BindAgentBindingRequest
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				op := value(body.OperationId)
				if value(body.Binding.ProfileId) != "profile/exact" || value(body.Binding.Speed) != "fast" || r.Header.Get("If-Match") != `"9007199254740993"` || r.Header.Get("Idempotency-Key") != op {
					t.Error("native selector or fencing lost")
				}
				if outcome == "drop" {
					drop(w)
					return
				}
				if outcome == "wrong-receipt" {
					op = "other-operation"
				}
				result := wire.CommandResult{OperationId: op, Outcome: wire.Outcome(outcome), Detail: pointer("private-sentinel"), Revision: pointer(wire.Uint64Decimal("9007199254740994"))}
				if outcome == "wrong-receipt" {
					result.Outcome = "committed"
				}
				if outcome == "conflicted" {
					w.WriteHeader(409)
				}
				writeFixture(w, result)
			})
			result, err := ChangeRuntimeConfiguration(t.Context(), settings, api.RuntimeConfigurationChange{Action: "bind", ID: "orbit", ExpectedRevision: "9007199254740993", Selection: api.WorkExecutionSettings{Model: "profile/exact", ServiceTier: "fast"}})
			want := outcome
			if outcome == "drop" || outcome == "wrong-receipt" {
				want = "unknown"
			}
			if err != nil || result.Outcome != want || posts.Load() != 1 || strings.Contains(result.Message, "private-sentinel") {
				t.Fatalf("receipt = %+v, %v, posts=%d", result, err, posts.Load())
			}
		})
	}
}
