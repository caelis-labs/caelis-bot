package caelis

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestExecutionEnvironmentIsExplicitWithoutCopyingHostValues(t *testing.T) {
	t.Setenv("BOT_EXECUTION_SECRET_FIXTURE", "not-a-real-secret")
	for _, toolsOnly := range []bool{false, true} {
		s := New(Options{Directory: t.TempDir(), ToolsOnly: toolsOnly})
		if err := s.ConfigureBotTools(&api.ToolConnection{Host: &acceptanceTools{defs: fixtureDefinitions("string")}, NotebookDirectory: "/fixture/notebook"}); err != nil {
			t.Fatal(err)
		}
		c := s.profile.ExecutionConfig
		if toolsOnly {
			if c != nil {
				t.Fatal("tools-only profile configured native execution")
			}
			continue
		}
		if c == nil || c.Environment == nil || c.Environment.Inherit == nil || !*c.Environment.Inherit || len(c.Environment.Set) != 0 || len(c.Environment.Unset) != 0 || c.Shell == nil || c.Shell.Login == nil || *c.Shell.Login {
			t.Fatal("wrong runtime assembly")
		}
		encoded, _ := json.Marshal(s.profile)
		if value(s.profile.Workspace.Cwd) != "/fixture/notebook" || !slices.Contains(required, "execution-configuration-v1") || strings.Contains(string(encoded), "not-a-real-secret") {
			t.Fatal("missing workspace/capability")
		}
	}
}
func TestOldHostCannotSilentlyUsePrivateExecutionEnvironment(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/initialize" {
			t.Fatal("unexpected mutation")
		}
		caps := slices.DeleteFunc(slices.Clone(required), func(v string) bool { return v == "execution-configuration-v1" })
		writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", Capabilities: caps})
	})
	if _, err := initialize(t.Context(), s.client); !errors.Is(err, errBotIncompatible) {
		t.Fatal("unsupported execution contract accepted", err)
	}
}
