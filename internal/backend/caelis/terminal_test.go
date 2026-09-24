package caelis

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestTerminalResolvesOnlyOwnedNativeWorkerWithoutDispatch(t *testing.T) {
	settings := setupFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("terminal resolution must not dispatch") })
	settings.CLIPath = filepath.Join(t.TempDir(), "caelis")
	if err := os.WriteFile(settings.CLIPath, []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Error("application must not dispatch") })
	s.settings = settings
	s.state.InstanceID = "setup-instance"
	w := worker{Native: true, Binding: wire.ApplicationBinding{ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", SessionId: "native-worker"}, Task: api.Task{ID: "owned", Workspace: t.TempDir()}}
	s.state.Workers["owned"] = w
	target, err := s.WorkTerminal(t.Context(), "owned")
	if err != nil || target.Runtime != "caelis" || target.Session != "native-worker" || target.Store != settings.CaelisStore || target.TokenFile != filepath.Join(settings.CaelisStore, "runtime/service/auth.token") {
		t.Fatal(target, err)
	}
	for _, id := range []string{"missing", "native-worker"} {
		if _, err = s.WorkTerminal(t.Context(), id); err == nil {
			t.Fatal("unowned target accepted")
		}
	}
	w.Native = false
	s.state.Workers["owned"] = w
	if _, err = s.WorkTerminal(t.Context(), "owned"); err == nil {
		t.Fatal("legacy application Session exposed as native Worker")
	}
	w.Native = true
	w.Binding.PrincipalId = "other"
	s.state.Workers["owned"] = w
	if _, err = s.WorkTerminal(t.Context(), "owned"); err == nil {
		t.Fatal("foreign scope accepted")
	}
	w.Binding.PrincipalId = "owner"
	s.state.Workers["owned"] = w
	s.state.InstanceID = "replaced"
	if _, err = s.WorkTerminal(t.Context(), "owned"); err == nil {
		t.Fatal("stale Host target accepted")
	}
}

func TestServiceChangeRefusesBusyUnknownAndChangedHost(t *testing.T) {
	for _, tt := range []struct {
		name, instance, body string
		wantError            bool
	}{
		{"idle old Host", "setup-instance", `{"runtime":{}}`, false},
		{"external Turn", "setup-instance", `{"runtime":{"active_sessions":["external"]}}`, true},
		{"background job", "setup-instance", `{"runtime":{"active_jobs":1}}`, true},
		{"running", "setup-instance", `{"runtime":{"running":true}}`, true},
		{"unknown", "setup-instance", `{}`, true},
		{"replaced", "different", `{"runtime":{}}`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Fatal("preflight mutated Host")
				}
				if r.URL.Path == "/api/control/v1/initialize" {
					writeFixture(w, map[string]any{"instance_id": tt.instance})
					return
				}
				_, _ = w.Write([]byte(tt.body))
			})
			if err := CheckServiceIdle(t.Context(), settings); (err != nil) != tt.wantError {
				t.Fatal(err)
			}
		})
	}
}
