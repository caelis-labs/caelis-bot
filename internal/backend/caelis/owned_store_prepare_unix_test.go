//go:build darwin || linux

package caelis

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func ownedPreparationParent(t *testing.T) string {
	t.Helper()
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(parent, 0700); e != nil {
		t.Fatal(e)
	}
	return parent
}
func TestPrepareOwnedStoreCreatesOnlyNewNodeMetadataBeforeAuthentication(t *testing.T) {
	parent := ownedPreparationParent(t)
	store := filepath.Join(parent, "new-store")
	if eligible, reason := ProbeOwnedStore("prepared-node", store); eligible || reason != "owned-store-setup-required" {
		t.Fatal(eligible, reason)
	}
	if e := PrepareOwnedStore("prepared-node", store); e != nil {
		t.Fatal(e)
	}
	if eligible, reason := ProbeOwnedStore("prepared-node", store); !eligible || reason != "" {
		t.Fatal("prepared metadata not recognized", eligible, reason)
	}
	marker, e := os.ReadFile(filepath.Join(store, ".caelis-bot-node-owner.json"))
	if e != nil {
		t.Fatal(e)
	}
	var node struct {
		NodeID string `json:"nodeId"`
	}
	if e = json.Unmarshal(marker, &node); e != nil || node.NodeID != "prepared-node" {
		t.Fatal(node, e)
	}
	entries, e := os.ReadDir(store)
	if e != nil || len(entries) != 2 {
		t.Fatal("preparation initialized runtime/auth state", entries, e)
	}
	for _, name := range []string{store, filepath.Join(store, ".native-home")} {
		info, e := os.Lstat(name)
		if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			t.Fatal("private native directory missing", name, e)
		}
	}
	info, e := os.Lstat(filepath.Join(store, ".caelis-bot-node-owner.json"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private exact marker missing", e)
	}
	// Human authentication may now add native state without losing ownership.
	if e = os.WriteFile(filepath.Join(store, "human-auth-fixture"), []byte("synthetic account material"), 0000); e != nil {
		t.Fatal(e)
	}
	if e = PrepareOwnedStore("prepared-node", store); e == nil {
		t.Fatal("existing prepared Store adopted/reused")
	}
	if eligible, reason := ProbeOwnedStore("another-node", store); eligible || reason != "owned-store-node-mismatch" {
		t.Fatal("marker scope lost", eligible, reason)
	}
	after, e := os.ReadFile(filepath.Join(store, ".caelis-bot-node-owner.json"))
	if e != nil || string(after) != string(marker) {
		t.Fatal("existing node marker changed", e)
	}
}
func TestPrepareOwnedStoreFailsClosedWithoutAdoptingExistingPaths(t *testing.T) {
	for _, scenario := range []string{"unmarked", "marked-other", "file", "shared-default", "store-symlink", "parent-symlink", "missing-parent", "unclean", "invalid-node", "parent-writers"} {
		t.Run(scenario, func(t *testing.T) {
			parent := ownedPreparationParent(t)
			store := filepath.Join(parent, "store")
			nodeID := "node-test"
			switch scenario {
			case "unmarked":
				if e := os.Mkdir(store, 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(filepath.Join(store, "auth-fixture"), []byte("synthetic-only"), 0000); e != nil {
					t.Fatal(e)
				}
			case "marked-other":
				if e := PrepareOwnedStore("other-node", store); e != nil {
					t.Fatal(e)
				}
			case "file":
				if e := os.WriteFile(store, []byte("existing"), 0600); e != nil {
					t.Fatal(e)
				}
			case "shared-default":
				t.Setenv("HOME", parent)
				store = filepath.Join(parent, ".caelis")
			case "store-symlink":
				if e := os.Symlink(parent, store); e != nil {
					t.Fatal(e)
				}
			case "parent-symlink":
				link := filepath.Join(parent, "alias")
				if e := os.Symlink(parent, link); e != nil {
					t.Fatal(e)
				}
				store = filepath.Join(link, "new-store")
			case "missing-parent":
				store = filepath.Join(parent, "missing", "store")
			case "unclean":
				store = parent + "/../" + filepath.Base(parent) + "/store"
			case "invalid-node":
				nodeID = "node\ninvalid"
			case "parent-writers":
				if e := os.Chmod(parent, 0777); e != nil {
					t.Fatal(e)
				}
			}
			if e := PrepareOwnedStore(nodeID, store); e == nil {
				t.Fatal("unsafe existing/redirected Store accepted")
			}
			if scenario == "unmarked" {
				if _, e := os.Lstat(filepath.Join(store, ".caelis-bot-node-owner.json")); !os.IsNotExist(e) {
					t.Fatal("unmarked authenticated Store adopted", e)
				}
			}
			if scenario == "shared-default" || scenario == "invalid-node" || scenario == "parent-writers" {
				if _, e := os.Lstat(store); !os.IsNotExist(e) {
					t.Fatal("failed preparation wrote Store", e)
				}
			}
		})
	}
}
