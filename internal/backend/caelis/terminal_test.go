package caelis

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS == "windows" {
		settings.CLIPath, _ = os.Executable()
	}
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Error("application must not dispatch") })
	s.settings = settings
	s.state.InstanceID = "setup-instance"
	w := worker{Native: true, Binding: wire.SessionBinding{PrincipalId: "owner", SessionId: "native-worker"}, Task: api.Task{ID: "owned", Workspace: t.TempDir()}}
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
