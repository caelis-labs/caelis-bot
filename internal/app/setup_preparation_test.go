package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func preparedHost(t *testing.T) (api.SetupState, *atomic.Int32) {
	t.Helper()
	revision := new(atomic.Int32)
	revision.Store(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/status" {
			t.Errorf("cache repeated setup RPC: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fixture" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"configuration": map[string]any{"revision": strconv.Itoa(int(revision.Load()))}})
	}))
	t.Cleanup(server.Close)
	store := t.TempDir()
	service := filepath.Join(store, "runtime", "service")
	if err := os.MkdirAll(service, 0700); err != nil {
		t.Fatal(err)
	}
	if err := localstate.Write(filepath.Join(service, "discovery.json"), map[string]any{"schema_version": "caelis.control.service-discovery/v1", "endpoint": server.URL, "instance_id": "fixture-host", "principal_id": "fixture-owner"}); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(service, "auth.token")
	if err := os.WriteFile(token, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := localstate.ProtectFile(token); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return api.SetupState{Settings: api.RuntimeSettings{Runtime: "caelis", CLIPath: binary, CaelisStore: store}, Installation: api.RuntimeStatus{Installed: true, Path: binary}, State: "ready", SelectedModel: "fixture/model", Models: []api.SetupChoice{{Value: "fixture/model"}}}, revision
}

func TestSetupPreparationReusesCatalogButInvalidatesConfiguration(t *testing.T) {
	a, _, _ := newSwitchFixture(t, false, nil)
	state, revision := preparedHost(t)
	s := a.setup
	s.rememberPreparation(t.Context(), state)
	p := s.prepared["caelis"]
	if p == nil {
		t.Fatal("successful setup not cached")
	}
	for range 2 {
		got, err := s.Apply(t.Context(), api.SetupRequest{Settings: state.Settings, Action: "prepare"})
		if err != nil || got.SelectedModel != "fixture/model" {
			t.Fatal("catalog not reused", got, err)
		}
	}
	revision.Add(1)
	if s.preparationValid(t.Context(), p, state.Settings) {
		t.Fatal("Host configuration change ignored")
	}
	revision.Store(1)
	dir, _ := providerDirectory(a.root, "caelis")
	if err := localstate.Write(filepath.Join(dir, "execution.json"), api.ExecutionSettings{Model: "another/model"}); err != nil {
		t.Fatal(err)
	}
	if s.preparationValid(t.Context(), p, state.Settings) {
		t.Fatal("Bot model change ignored")
	}
	s.rememberPreparation(t.Context(), state)
	p = s.prepared["caelis"]
	p.until = time.Now().Add(-time.Second)
	if s.preparationValid(t.Context(), p, state.Settings) {
		t.Fatal("expired snapshot reused")
	}
	s.rememberPreparation(t.Context(), api.SetupState{Settings: state.Settings, State: "unavailable"})
	if s.prepared["caelis"] != nil {
		t.Fatal("unavailable setup retained")
	}
}

func TestSetupActivationReusesOnlyItsUnchangedReadyResident(t *testing.T) {
	a, _, engines := newSwitchFixture(t, false, nil)
	state, _ := preparedHost(t)
	if err := a.activateRuntime(t.Context(), state.Settings, true); err != nil {
		t.Fatal(err)
	}
	s := a.setup
	s.rememberPreparation(t.Context(), state)
	s.activePreparation = s.prepared["caelis"]
	s.activeEngine = a.currentEngine()
	before := len(*engines)
	if err := s.Activate(t.Context(), state.Settings); err != nil {
		t.Fatal(err)
	}
	if len(*engines) != before {
		t.Fatal("returning through onboarding recreated the resident")
	}
	s.activeEngine = nil
	if s.canReuseRuntime(state.Settings) {
		t.Fatal("different resident reused")
	}
	s.activeEngine = a.currentEngine()
	engine := s.activeEngine.(*switchEngine)
	engine.mu.Lock()
	engine.ready = false
	engine.mu.Unlock()
	if s.canReuseRuntime(state.Settings) {
		t.Fatal("offline resident accepted")
	}
}

func TestSetupFileFenceDetectsReplacementAndMissingFiles(t *testing.T) {
	file := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(file, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(file)
	a := []setupFile{{file, before}}
	if err := os.WriteFile(file, []byte("changed-credentials"), 0600); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(file)
	if sameSetupFiles(a, []setupFile{{file, after}}) || sameSetupFiles(a, []setupFile{{file, nil}}) {
		t.Fatal("credential change ignored")
	}
}
