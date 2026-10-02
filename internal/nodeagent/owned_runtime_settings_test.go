package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

func ownedSettingsBinary(t *testing.T, directory, name string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("synthetic executable; never launched"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOwnedRuntimeSettingsProjectsOnlyDesignatedNativeMetadata(t *testing.T) {
	s := agentFixture(t)
	binary := ownedSettingsBinary(t, s.options.Directory, "caelis")
	store := filepath.Join(s.options.Directory, "existing-config-designated-store")
	s.options.Binaries[api.NodeCaelis] = binary
	s.options.Configurations = map[api.NodeBackend]NativeConfiguration{api.NodeCaelis: &CaelisConfiguration{Settings: api.RuntimeSettings{Runtime: "caelis", CLIPath: "/not-authority", CaelisStore: store}}}
	value, err := s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil || value != (OwnedRuntimeSettings{Backend: api.NodeCaelis, Binary: binary, Store: store}) {
		t.Fatal(value, err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields["backend"] != "caelis" || fields["binary"] != binary || fields["store"] != store {
		t.Fatal("native DTO widened beyond the three nonsecret path fields", fields)
	}
	if _, err := os.Stat(store); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("metadata query created or opened the designated Store", err)
	}
	s.options.Configurations = nil
	value, err = s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil || value.Store != filepath.Join(s.options.Directory, "caelis-store") {
		t.Fatal("fixed target Store not designated", value, err)
	}
	if _, err := os.Stat(value.Store); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fallback query initialized a Store", err)
	}
	s.options.Binaries[api.NodeCodex] = binary
	value, err = s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, api.NodeCodex)
	if err != nil || value.Backend != api.NodeCodex || value.Store != "" {
		t.Fatal("Codex metadata acquired Store authority", value, err)
	}
}

type ownedSettingsInstaller struct {
	installationFixture
	path  string
	err   error
	reads int
}

func (i *ownedSettingsInstaller) BinaryPath(string) (string, error) { i.reads++; return i.path, i.err }

func TestOwnedRuntimeSettingsInstalledExecutableAndMissingAreHonest(t *testing.T) {
	s := agentFixture(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	installed := ownedSettingsBinary(t, s.options.Directory, "installed-caelis")
	explicit := ownedSettingsBinary(t, s.options.Directory, "explicit-caelis")
	s.options.Binaries[api.NodeCaelis] = explicit
	installer := &ownedSettingsInstaller{path: installed}
	s.installation = installer
	value, err := s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil || value.Binary != installed || installer.reads != 1 {
		t.Fatal("installed selection was not retained", value, err)
	}
	installer.path, installer.err = "", errors.New("not installed")
	value, err = s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil || value.Binary != explicit {
		t.Fatal("explicit target binary was not available", value, err)
	}
	for _, path := range []string{"", filepath.Join(s.options.Directory, "missing"), s.options.Directory} {
		s.options.Binaries[api.NodeCaelis] = path
		if value, err := s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, api.NodeCaelis); err == nil || value != (OwnedRuntimeSettings{}) {
			t.Fatal("missing or non-executable target binary returned settings", value, err)
		}
	}
	if err := os.Chmod(explicit, 0600); err != nil {
		t.Fatal(err)
	}
	s.options.Binaries[api.NodeCaelis] = explicit
	if _, err := s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, api.NodeCaelis); err == nil {
		t.Fatal("non-executable file became a runtime")
	}
}

func TestMachineRuntimeSettingsUseLocalBinAndPersistDesignation(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python setup fixture unavailable")
	}
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/codex_setup_fixture.py")
	if err != nil {
		t.Fatal(err)
	}
	fixture = bytes.Replace(fixture, []byte("#!/usr/bin/env python3"), []byte("#!"+python), 1)
	standard := filepath.Join(dir, "codex")
	if err = os.WriteFile(standard, fixture, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "empty-codex-home"))
	t.Setenv("CODEX_BIN", "")
	t.Setenv("PATH", t.TempDir())
	s := agentFixture(t)
	s.options.Binaries = map[api.NodeBackend]string{}
	s.options.Configurations[api.NodeCodex] = &CodexConfiguration{Directory: s.options.Directory}
	value, err := s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, api.NodeCodex)
	if err != nil || value.Binary != standard {
		t.Fatal("noninteractive local-bin discovery", value, err)
	}
	configuration, err := s.Configuration(t.Context(), s.options.NodeID, api.NodeCodex)
	if err != nil || !configuration.Executable.Installed {
		t.Fatal("native detection disagrees with resolved Runtime", err)
	}
	designated := filepath.Join(home, "designated-codex")
	if err = os.WriteFile(designated, fixture, 0700); err != nil {
		t.Fatal(err)
	}
	request := api.NodeRuntimeSettingsRequest{Guard: configuration.Guard, Settings: api.RuntimeSettings{Runtime: "codex", CLIPath: designated}}
	if result, err := s.SaveOwnedRuntimeSettings(t.Context(), request); err != nil || !result.Saved {
		t.Fatal("native designation save", result, err)
	}
	restarted, err := New(Options{Directory: s.options.Directory, NodeID: s.options.NodeID})
	if err != nil {
		t.Fatal(err)
	}
	value, err = restarted.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, api.NodeCodex)
	if err != nil || value.Binary != designated {
		t.Fatal("restart lost designated machine CLI", value, err)
	}
	configuration, err = restarted.Configuration(t.Context(), s.options.NodeID, api.NodeCodex)
	if err != nil || !configuration.Executable.Installed {
		t.Fatal("restart detection lost designation", err)
	}
	request.Guard = configuration.Guard
	request.Settings.CLIPath = filepath.Join(home, "missing")
	if _, err = restarted.SaveOwnedRuntimeSettings(t.Context(), request); err == nil {
		t.Fatal("missing designation was saved")
	}
	value, err = restarted.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, api.NodeCodex)
	if err != nil || value.Binary != designated {
		t.Fatal("failed check replaced designated Runtime", value, err)
	}
}

func TestOwnedRuntimeSettingsUsesNativeConfiguredAndStandardInstalledCLIs(t *testing.T) {
	for _, backend := range []api.NodeBackend{api.NodeCodex, api.NodeCaelis} {
		t.Run(string(backend), func(t *testing.T) {
			s := agentFixture(t)
			s.options.Binaries = nil
			searchDirectory := t.TempDir()
			standard := ownedSettingsBinary(t, searchDirectory, string(backend))
			t.Setenv("PATH", searchDirectory)
			configured := ownedSettingsBinary(t, s.options.Directory, "configured-"+string(backend))
			if backend == api.NodeCodex {
				s.options.Configurations = map[api.NodeBackend]NativeConfiguration{backend: &CodexConfiguration{Binary: configured}}
			} else {
				s.options.Configurations = map[api.NodeBackend]NativeConfiguration{backend: &CaelisConfiguration{Settings: api.RuntimeSettings{Runtime: "caelis", CLIPath: configured}}}
			}
			value, err := s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, backend)
			if err != nil || value.Binary != configured {
				t.Fatal("native configured binary was ignored", value, err)
			}
			s.options.Configurations = nil
			for _, managed := range []installer{nil, &ownedSettingsInstaller{err: errors.New("not installed")}} {
				s.installation = managed
				value, err = s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, backend)
				if err != nil || value.Binary != standard {
					t.Fatal("standard native CLI discovery was not preserved", value, err)
				}
				if backend == api.NodeCodex && value.Store != "" || backend == api.NodeCaelis && value.Store != filepath.Join(s.options.Directory, "caelis-store") {
					t.Fatal("CLI discovery changed designated Store scope", value)
				}
				if value.Store != "" {
					if _, err := os.Stat(value.Store); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("discovery initialized Store", err)
					}
				}
			}
			// A configured target or authoritative provider must never silently
			// switch to a different installed CLI when it fails.
			s.options.Binaries = map[api.NodeBackend]string{backend: filepath.Join(s.options.Directory, "missing-selected-cli")}
			if value, err := s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, backend); err == nil || value != (OwnedRuntimeSettings{}) {
				t.Fatal("selected missing CLI fell back to PATH", value, err)
			}
			s.options.OwnedRuntimeSettings = func(context.Context, api.NodeBackend) (OwnedRuntimeSettings, error) {
				return OwnedRuntimeSettings{Backend: backend, Binary: standard, Store: filepath.Join(s.options.Directory, "store")}, errors.New("arbitrary native provider failure")
			}
			if value, err := s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, backend); err == nil || value != (OwnedRuntimeSettings{}) {
				t.Fatal("authoritative provider error fell back", value, err)
			}
		})
	}
}

func TestOwnedRuntimeSettingsExactScopeStrictInputsAndHookValidation(t *testing.T) {
	s := agentFixture(t)
	binary := ownedSettingsBinary(t, s.options.Directory, "caelis")
	value := OwnedRuntimeSettings{Backend: api.NodeCaelis, Binary: binary, Store: filepath.Join(s.options.Directory, "native-known-store")}
	var calls atomic.Int32
	s.options.OwnedRuntimeSettings = func(context.Context, api.NodeBackend) (OwnedRuntimeSettings, error) { calls.Add(1); return value, nil }
	for _, req := range []ownedRuntimeSettingsRequest{{"other-node", api.NodeCaelis}, {s.options.NodeID, "unknown"}} {
		if _, err := s.ReadOwnedRuntimeSettings(t.Context(), req.NodeID, req.Backend); err == nil {
			t.Fatal("scope substitution accepted")
		}
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.ReadOwnedRuntimeSettings(canceled, s.options.NodeID, api.NodeCaelis); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled read reached native hook", err)
	}
	if calls.Load() != 0 {
		t.Fatal("invalid scope reached native hook")
	}
	for _, body := range []string{`{"nodeId":"node-test","backend":"caelis","store":"/arbitrary"}`, `{"nodeId":"node-test","backend":"caelis","token":"synthetic"}`, `{"nodeId":"node-test","backend":"caelis"} {}`, `{"nodeId":"other-node","backend":"caelis"}`} {
		response := httptest.NewRecorder()
		Handler(s).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/node/owned-runtime-settings", bytes.NewBufferString(body)))
		if response.Code == http.StatusOK {
			t.Fatal("unknown input or identity acquired authority")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("untrusted fields reached native hook")
	}
	got, err := s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil || got != value {
		t.Fatal("native known Store metadata unavailable", got, err)
	}
	for _, change := range []func(*OwnedRuntimeSettings){func(v *OwnedRuntimeSettings) { v.Backend = api.NodeCodex }, func(v *OwnedRuntimeSettings) { v.Binary = "relative" }, func(v *OwnedRuntimeSettings) { v.Store = "/store/../redirected" }, func(v *OwnedRuntimeSettings) { v.Store = "http://host/store" }, func(v *OwnedRuntimeSettings) { v.Store = "/store\ncommand" }} {
		invalid := value
		change(&invalid)
		s.options.OwnedRuntimeSettings = func(context.Context, api.NodeBackend) (OwnedRuntimeSettings, error) { return invalid, nil }
		if _, err := s.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, api.NodeCaelis); err == nil {
			t.Fatal("native hook bypassed metadata validation", invalid)
		}
	}
}

func TestOwnedRuntimeSettingsPinnedClientOverNativeIPC(t *testing.T) {
	s := agentFixture(t)
	binary := ownedSettingsBinary(t, s.options.Directory, "caelis")
	s.options.Binaries[api.NodeCaelis] = binary
	local, remote := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- productrpc.ServeNativeStream(t.Context(), remote, remote, Handler(s), allowed) }()
	c, err := NewClient(s.options.NodeID, local)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(); <-done })
	value, err := c.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil || value.Binary != binary || value.Store != filepath.Join(s.options.Directory, "caelis-store") {
		t.Fatal("native settings IPC failed", value, err)
	}
	if _, err := c.ReadOwnedRuntimeSettings(t.Context(), "other-node", api.NodeCaelis); err == nil {
		t.Fatal("client changed its pinned target")
	}
	if _, err := c.ReadOwnedRuntimeSettings(t.Context(), s.options.NodeID, "unknown"); err == nil {
		t.Fatal("client widened native backend scope")
	}
}

func TestOwnedRuntimeSettingsClientRejectsInvalidOrExpandedMetadata(t *testing.T) {
	for _, response := range []string{`{"backend":"codex","binary":"/target/bin/codex","store":""}`, `{"backend":"caelis","binary":"relative","store":"/target/store"}`, `{"backend":"caelis","binary":"/target/bin/caelis","store":"/target/store","token":"synthetic"}`} {
		t.Run(response, func(t *testing.T) {
			local, remote := net.Pipe()
			done := make(chan error, 1)
			go func() {
				done <- productrpc.ServeNativeStream(t.Context(), remote, remote, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(response)) }), allowed)
			}()
			c, err := NewClient("node-test", local)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close(); <-done }()
			if _, err := c.ReadOwnedRuntimeSettings(t.Context(), "node-test", api.NodeCaelis); err == nil {
				t.Fatal("invalid target metadata accepted")
			}
		})
	}
}
