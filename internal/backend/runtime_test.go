package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRuntimeSettingsPersistOnlyAfterValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.json")
	s := NewService(snapshotEngine{}, nil, nil, nil, nil)
	initial := api.RuntimeSettings{Runtime: "codex"}
	valid := false
	s.ConfigureRuntime(path, initial, func(_ context.Context, _ string, persist func() error) (api.RuntimeCheck, error) {
		if !valid {
			return api.RuntimeCheck{}, errors.New("invalid executable")
		}
		return api.RuntimeCheck{Saved: true}, persist()
	})
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
	loaded, err := LoadRuntimeSettings(path)
	if err != nil || loaded != selected {
		t.Fatal("next startup lost the validated path", err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("settings permissions")
	}
	if _, err := s.SaveRuntimeSettings(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadRuntimeSettings(path); err != nil || loaded.CLIPath != "" {
		t.Fatal("could not return to automatic discovery")
	}
}
