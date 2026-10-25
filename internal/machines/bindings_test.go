package machines

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBindingFailureNeverLeavesAnUnpersistedRoute(t *testing.T) {
	s, _ := Open(t.TempDir(), inert{})
	s.state.Profiles["machine-owned"] = profile{View: api.Machine{Runtime: "codex", State: "ready", PairingStatus: "approved", CheckedAt: time.Now().UnixMilli()}}
	s.root = filepath.Join(s.root, "unwritable")
	if e := os.WriteFile(s.root, []byte("file"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := s.BindWork(t.Context(), api.TaskStart{Machine: "machine-owned"}, "task-original", ""); e == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if len(s.state.Routes) != 0 || len(s.state.Runtimes) != 0 {
		t.Fatal("failed binding retained in memory")
	}
}
