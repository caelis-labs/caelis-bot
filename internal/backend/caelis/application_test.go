package caelis

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestApplicationModeNeverFallsBackToLegacyBot(t *testing.T) {
	dir := t.TempDir()
	s := New(Options{Directory: dir, ApplicationOwned: true, Settings: api.RuntimeSettings{Runtime: "caelis", CLIPath: "/must-not-execute/caelis"}})
	if e := s.ConfigureBotTools(&api.ToolConnection{Command: "synthetic"}); e != nil {
		t.Fatal(e)
	}
	if e := s.Connect(t.Context()); !errors.Is(e, errApplicationProtocol) {
		t.Fatal(e)
	}
	if s.connected || s.client != nil || s.Snapshot().Connection == "ready" {
		t.Fatal("legacy connection activated")
	}
	if _, e := os.Stat(filepath.Join(dir, "binding.json")); !os.IsNotExist(e) {
		t.Fatal("legacy binding created")
	}
	if _, e := s.StartWork(t.Context(), api.WorkStart{}); !errors.Is(e, errApplicationProtocol) {
		t.Fatal("unavailable work accepted")
	}
}
