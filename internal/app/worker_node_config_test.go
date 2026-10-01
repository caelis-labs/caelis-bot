package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestWorkerStartupOfflinePreflightUsesExistingPrivateSchema(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "worker-nodes.json")
	configs := []backend.WorkerNodeConfig{{ID: "same-node", Label: "Synthetic", SSH: "fixture", Backend: "caelis", Store: "/tmp/owned/store", WorkspaceRoot: "/tmp/owned/work"}, {ID: "same-node", Label: "Synthetic", SSH: "fixture", Backend: "codex", Socket: "/tmp/owned/worker.sock", WorkspaceRoot: "/tmp/owned/work"}}
	b, _ := json.Marshal(workerNodeDocument{Version: 1, Nodes: configs})
	selected := []api.WorkTarget{{NodeID: "same-node", Backend: "caelis", Role: api.RoleWorker}, {NodeID: "same-node", Backend: "codex", Role: api.RoleWorker}}
	save := func(value []byte, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, value, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	save(b, 0600)
	if err := ValidateConfiguredWorkerTargets(root, selected); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append(append([]byte{}, b...), []byte(` {}`)...), []byte(`{"version":1,"nodes":[],"unknown":true}`), []byte(`{"version":2,"nodes":[]}`)} {
		save(bad, 0600)
		if ValidateConfiguredWorkerTargets(root, selected) == nil {
			t.Fatal("unsafe schema accepted")
		}
		if ValidateConfiguredWorkerTargets(root, nil) != nil {
			t.Fatal("default optional config became prerequisite")
		}
		controller := openWorkerNodes(path, nil, nil)
		if controller.issue != "config_unreadable" {
			t.Fatal("ordinary APP tolerance changed")
		}
		controller.cancel()
	}
	save(b, 0644)
	if ValidateConfiguredWorkerTargets(root, selected) == nil {
		t.Fatal("public config accepted")
	}
	save(b, 0600)
	if ValidateConfiguredWorkerTargets(root, append(selected, selected[0])) == nil {
		t.Fatal("duplicate selection accepted")
	}
	if err := os.Rename(path, path+".regular"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".regular", path); err != nil {
		t.Fatal(err)
	}
	if ValidateConfiguredWorkerTargets(root, selected) == nil {
		t.Fatal("linked config accepted")
	}
}
