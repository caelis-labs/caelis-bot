package caelis

import (
	"net/http"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestWorkModelResolvesHostDefaultWithoutInheritingBot(t *testing.T) {
	fallback := wire.ApplicationProfile{Model: "bot-luna", ReasoningEffort: pointer("low"), ServiceTier: pointer("priority")}
	for _, tc := range []struct {
		name   string
		model  wire.StatusModel
		custom api.WorkExecutionSettings
		status int
		fail   bool
		want   api.WorkExecutionSettings
	}{
		{name: "runtime without reasoning catalog", model: wire.StatusModel{Alias: pointer("Runtime Sol"), ReasoningEffort: pointer("high"), FastMode: pointer(true)}, want: api.WorkExecutionSettings{Model: "provider/sol", Effort: "high", ServiceTier: "priority"}},
		{name: "runtime unset", want: api.WorkExecutionSettings{Model: "bot-luna", Effort: "low", ServiceTier: "priority"}},
		{name: "manual bypasses runtime", custom: api.WorkExecutionSettings{Model: "manual-sol", Effort: "medium"}, status: 503, want: api.WorkExecutionSettings{Model: "manual-sol", Effort: "medium"}},
		{name: "runtime unavailable", status: 503, fail: true},
		{name: "configured but missing auth", model: wire.StatusModel{Alias: pointer("Runtime Sol"), MissingApiKey: pointer(true)}, fail: true},
		{name: "configured but missing catalog", model: wire.StatusModel{Alias: pointer("removed")}, fail: true},
		{name: "incomplete model identity", model: wire.StatusModel{Name: pointer("sol")}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.custom.Model != "" {
					t.Error("manual work queried global model")
				}
				if r.Method != "GET" && !strings.HasSuffix(r.URL.Path, "/completion/slash-arguments") {
					t.Error("resolution mutated Host", r.URL.Path)
				}
				switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
				case "/initialize":
					writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("setup-instance"), Capabilities: required})
				case "/status":
					if tc.status != 0 {
						w.WriteHeader(tc.status)
						return
					}
					writeFixture(w, wire.StatusSnapshot{Configuration: wire.StatusConfiguration{Revision: "1"}, ModelStatus: tc.model})
				case "/completion/slash-arguments":
					if tc.name == "runtime unset" {
						writeFixture(w, []wire.SlashArgCandidate{})
						return
					}
					// Models without reasoning capabilities omit ModelSelection entirely.
					writeFixture(w, []wire.SlashArgCandidate{{Value: "provider/sol", Display: pointer("Runtime Sol")}})
				default:
					t.Error("unexpected model resolution route", r.URL.Path)
				}
			})
			s := New(Options{Directory: t.TempDir(), Settings: settings, WorkExecution: tc.custom})
			if tc.name == "runtime without reasoning catalog" {
				models, err := s.Models(t.Context())
				if err != nil || len(models) != 1 || models[0].Efforts == nil {
					t.Fatal("non-reasoning model must expose an empty UI array", err)
				}
			}
			got, err := s.resolveWorkExecution(t.Context(), fallback)
			if tc.fail {
				if err == nil {
					t.Fatal("configuration error silently fell back", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}
