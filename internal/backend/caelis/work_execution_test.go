package caelis

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestWorkModelResolvesHostDefaultWithoutInheritingBot(t *testing.T) {
	fallback := wire.SessionProfile{Model: "bot-luna", ReasoningEffort: pointer("low"), ServiceTier: pointer("priority")}
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
			if tc.custom.Model == "" {
				s.workExecution = api.WorkExecutionSettings{Model: "work-override"}
				configured, readErr := s.RuntimeDefault(t.Context())
				expected := tc.want
				if tc.name == "runtime unset" {
					expected = api.WorkExecutionSettings{}
				}
				if readErr != nil || configured != expected {
					t.Fatalf("configured default = %+v, %v; want %+v", configured, readErr, expected)
				}
			}
		})
	}
}

func TestConversationWorkerUsesSelectedNativeModelInheritedEffort(t *testing.T) {
	for _, effort := range []string{"", "low", "invalid-explicit"} {
		t.Run("effort-"+effort, func(t *testing.T) {
			settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
				path := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
				switch path {
				case "/initialize":
					writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("setup-instance"), Capabilities: required})
				case "/sessions/native-owned/configuration":
					var p *string
					if effort != "" {
						p = pointer(effort)
					}
					writeFixture(w, wire.SessionConfiguration{SessionId: "native-owned", Revision: "r1", Profile: wire.SessionProfile{Model: "selected", ReasoningEffort: p}})
				case "/completion/slash-arguments":
					writeFixture(w, []wire.SlashArgCandidate{{Value: "other", ModelSelection: &wire.ModelSelection{Current: pointer(true), Effort: "max", Efforts: []string{"max"}}}, {Value: "selected", ModelSelection: &wire.ModelSelection{Effort: "high", Efforts: []string{"none", "low", "high", "max"}}}})
				default:
					t.Errorf("read-only resolution dispatched unexpected route %s", path)
				}
			})
			directory := t.TempDir()
			if err := os.Chmod(directory, 0700); err != nil {
				t.Fatal(err)
			}
			s := New(Options{Directory: directory, Settings: settings})
			c, err := setupClient(t.Context(), settings)
			if err != nil {
				t.Fatal(err)
			}
			defer c.http.CloseIdleConnections()
			s.client = c
			s.state.Session.SessionId = "native-owned"
			got, err := s.ConversationExecution(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			want := effort
			if want == "" {
				want = "high"
			}
			if got.Model != "selected" || got.Effort != want {
				t.Fatal("inherited default switched model/explicit effort", got)
			}
			models, err := s.Models(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			invalid := api.ValidateWorkExecution(got, models)
			if effort == "invalid-explicit" {
				if api.WorkerReadinessCode(invalid) != "worker_effort_unsupported" {
					t.Fatal(invalid)
				}
			} else if invalid != nil {
				t.Fatal(invalid)
			}
		})
	}
}
