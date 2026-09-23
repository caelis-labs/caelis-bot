package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type runtimeFake struct {
	snapshotEngine
	change func(context.Context, api.RuntimeSettings, func() error) (api.RuntimeCheck, error)
}

func (e *runtimeFake) ChangeRuntime(ctx context.Context, v api.RuntimeSettings, persist func() error) (api.RuntimeCheck, error) {
	return e.change(ctx, v, persist)
}

func TestRuntimeSettingsPersistOnlyAfterValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.json")
	engine := &runtimeFake{}
	s := NewService(engine, nil, nil, nil, nil)
	initial := api.RuntimeSettings{Runtime: "codex"}
	valid := false
	s.ConfigureRuntime(path, initial)
	engine.change = func(_ context.Context, _ api.RuntimeSettings, persist func() error) (api.RuntimeCheck, error) {
		if !valid {
			return api.RuntimeCheck{}, errors.New("invalid executable")
		}
		return api.RuntimeCheck{Saved: true}, persist()
	}
	selected := api.RuntimeSettings{Runtime: "codex", CLIPath: "/local/bin/codex"}
	if _, err := s.SaveRuntimeSettings(context.Background(), selected); err == nil {
		t.Fatal("accepted failed validation")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) || s.RuntimeSettings() != initial {
		t.Fatal("failed validation changed configuration")
	}
	valid = true
	if _, err := s.SaveRuntimeSettings(context.Background(), selected); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRuntimeSettings(path, "codex")
	if err != nil || loaded != selected {
		t.Fatal("next startup lost the validated path", err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("settings permissions")
	}
	if _, err := s.SaveRuntimeSettings(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadRuntimeSettings(path, "codex"); err != nil || loaded.CLIPath != "" {
		t.Fatal("could not return to automatic discovery")
	}
}

func TestProviderMismatchNeverReachesActiveAdapter(t *testing.T) {
	called := false
	e := &runtimeFake{change: func(context.Context, api.RuntimeSettings, func() error) (api.RuntimeCheck, error) {
		called = true
		return api.RuntimeCheck{}, nil
	}}
	s := NewService(e, nil, nil, nil, nil)
	s.ConfigureRuntime(filepath.Join(t.TempDir(), "runtime.json"), api.RuntimeSettings{Runtime: "fixture"})
	if _, err := s.SaveRuntimeSettings(context.Background(), api.RuntimeSettings{Runtime: "codex"}); err == nil || called {
		t.Fatal("cross-provider change reached active adapter")
	}
}
func TestRuntimeDocumentsAreVersionedAndProviderNeutral(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.json")
	for _, doc := range []string{`{"runtime":"fixture","cliPath":""}`, `{"version":1,"runtime":"fixture","cliPath":""}`} {
		if err := os.WriteFile(path, []byte(doc), 0600); err != nil {
			t.Fatal(err)
		}
		v, err := LoadRuntimeSettings(path, "codex")
		if err != nil || v.Runtime != "fixture" {
			t.Fatal("provider-specific loader", err)
		}
	}
	if err := os.WriteFile(path, []byte(`{"version":2,"runtime":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRuntimeSettings(path, "codex"); err == nil {
		t.Fatal("unknown document version accepted")
	}
}

type providerRuntimeFake struct{ runtimeFake }

func (*providerRuntimeFake) ProviderInfo() api.ProviderInfo { return api.ProviderInfo{ID: "codex"} }
func TestPendingProviderSettingsNeverReachActiveAdapter(t *testing.T) {
	e := &providerRuntimeFake{}
	e.change = func(context.Context, api.RuntimeSettings, func() error) (api.RuntimeCheck, error) {
		t.Fatal("pending Caelis settings reached Codex")
		return api.RuntimeCheck{}, nil
	}
	s := NewService(e, nil, nil, nil, nil)
	path := filepath.Join(t.TempDir(), "runtime.json")
	initial := api.RuntimeSettings{Runtime: "codex"}
	s.ConfigureRuntime(path, initial)
	blocked := true
	probes := 0
	s.ConfigureRuntimeManagement(nil, func(context.Context, api.RuntimeSettings) error { probes++; return nil }, nil, func() error {
		if blocked {
			return errors.New("busy")
		}
		return nil
	})
	pending := api.RuntimeSettings{Runtime: "caelis", CaelisStore: "/isolated/store"}
	if _, err := s.SaveRuntimeSettings(t.Context(), pending); err == nil || probes != 0 || s.RuntimeSettings() != initial {
		t.Fatal("busy owner replaced")
	}
	blocked = false
	for range 2 {
		if _, err := s.SaveRuntimeSettings(t.Context(), pending); err != nil {
			t.Fatal(err)
		}
	}
	if probes != 2 {
		t.Fatal("second pending save skipped its provider probe")
	}
	loaded, err := LoadRuntimeSettings(path, "codex")
	if err != nil || loaded != pending {
		t.Fatal("next-start configuration missing")
	}
}
