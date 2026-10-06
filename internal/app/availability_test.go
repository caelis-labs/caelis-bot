package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestCorruptOptionalStoresKeepApplicationAndControlsAvailable(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"bot-initialization.json", "task-preferences.json", "tasks.json", "worker-runtime.json", "telegram.json", "Machines/machines.json"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("original damaged bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "runtime.json"), []byte(`{"runtime":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	e := newTestEngine()
	a, err := newApplication(root, Host{}, func(id string) (providerFactory, error) {
		return providerFactory{ID: id, Open: func(providerConfig) (api.Engine, error) { return e, nil }}, nil
	})
	if err != nil {
		t.Fatal("optional error prevented shell assembly", err)
	}
	defer a.Close()
	if err := a.Start(); err != nil {
		t.Fatal("optional error prevented runtime observation", err)
	}
	waitSignal(t, e.connectSeen)
	started := time.Now()
	_ = a.Backend.Snapshot()
	_ = a.Backend.RecoveryState()
	_ = a.Telegram.Status()
	if time.Since(started) > time.Second || !a.started || a.tasks != nil {
		t.Fatal("optional fault disabled controls")
	}
	for _, name := range []string{"bot-initialization.json", "tasks.json", "worker-runtime.json", "telegram.json", "Machines/machines.json"} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(b) != "original damaged bytes" {
			t.Fatal("replaced unresolved identity/receipt", name, err)
		}
	}
}

func TestUnrecognizedRuntimeKeepsShellAndOriginalConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runtime.json")
	original := []byte(`{"version":1,"runtime":"unavailable-provider"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, Host{})
	if err != nil {
		t.Fatal("unsupported config prevented shell", err)
	}
	defer a.Close()
	_ = a.Backend.Snapshot()
	if err := a.Backend.Connect(t.Context()); err == nil {
		t.Fatal("guessed runtime became an execution owner")
	}
	if b, _ := os.ReadFile(path); string(b) != string(original) {
		t.Fatal("original runtime config replaced")
	}
}
