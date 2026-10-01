//go:build darwin || linux

package caelis

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func ownedStoreProbeFixture(t *testing.T) string {
	t.Helper()
	store, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store, 0700); err != nil {
		t.Fatal(err)
	}
	marker, _ := json.Marshal(struct {
		NodeID string `json:"nodeId"`
	}{"node-test"})
	if err := os.WriteFile(filepath.Join(store, ".caelis-bot-node-owner.json"), marker, 0600); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestOwnedStoreProbeIsReadOnlyAndDoesNotAdoptUnmarkedStores(t *testing.T) {
	store := ownedStoreProbeFixture(t)
	if eligible, reason := ProbeOwnedStore("node-test", store); !eligible || reason != "" {
		t.Fatal(eligible, reason)
	}
	entries, err := os.ReadDir(store)
	if err != nil || len(entries) != 1 || entries[0].Name() != ".caelis-bot-node-owner.json" {
		t.Fatal("probe initialized native state", entries, err)
	}
	if err := os.Remove(filepath.Join(store, ".caelis-bot-node-owner.json")); err != nil {
		t.Fatal(err)
	}
	if eligible, reason := ProbeOwnedStore("node-test", store); eligible || reason != "owned-store-setup-required" {
		t.Fatal("unmarked Store adopted", eligible, reason)
	}
	entries, err = os.ReadDir(store)
	if err != nil || len(entries) != 0 {
		t.Fatal("probe marked an existing Store", entries, err)
	}
	missing := filepath.Join(store, "missing-store")
	if eligible, reason := ProbeOwnedStore("node-test", missing); eligible || reason != "owned-store-setup-required" {
		t.Fatal("missing Store treated as ready", eligible, reason)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("probe created missing Store", err)
	}
	t.Setenv("HOME", store)
	if eligible, reason := ProbeOwnedStore("node-test", filepath.Join(store, ".caelis")); eligible || reason != "shared-default-store" {
		t.Fatal("default shared Store admitted", eligible, reason)
	}
}

func TestOwnedStoreProbeRejectsForeignMalformedOrRedirectedMetadata(t *testing.T) {
	for _, name := range []string{"node", "unknown-marker", "large-marker", "store-permissions", "marker-permissions", "marker-symlink", "store-symlink", "parent-symlink", "unclean", "relative", "lock-directory", "lock-permissions", "discovery", "runtime-symlink", "native-home-shared"} {
		t.Run(name, func(t *testing.T) {
			store := ownedStoreProbeFixture(t)
			marker := filepath.Join(store, ".caelis-bot-node-owner.json")
			switch name {
			case "node":
				if err := os.WriteFile(marker, []byte(`{"nodeId":"other-node"}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "unknown-marker":
				if err := os.WriteFile(marker, []byte(`{"nodeId":"node-test","controller":"foreign"}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "large-marker":
				if err := os.WriteFile(marker, []byte(strings.Repeat(" ", 4097)), 0600); err != nil {
					t.Fatal(err)
				}
			case "store-permissions":
				if err := os.Chmod(store, 0755); err != nil {
					t.Fatal(err)
				}
			case "marker-permissions":
				if err := os.Chmod(marker, 0644); err != nil {
					t.Fatal(err)
				}
			case "marker-symlink":
				if err := os.Rename(marker, filepath.Join(store, "other-marker")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("other-marker", marker); err != nil {
					t.Fatal(err)
				}
			case "store-symlink", "parent-symlink":
				parent, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(parent, "redirect")
				target := store
				if name == "parent-symlink" {
					target = filepath.Dir(store)
				}
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
				if name == "parent-symlink" {
					store = filepath.Join(link, filepath.Base(store))
				} else {
					store = link
				}
			case "unclean":
				store += "/../" + filepath.Base(store)
			case "relative":
				store = "relative-store"
			case "lock-directory":
				if err := os.Mkdir(filepath.Join(store, ".caelis-bot-node.lock"), 0700); err != nil {
					t.Fatal(err)
				}
			case "lock-permissions":
				if err := os.WriteFile(filepath.Join(store, ".caelis-bot-node.lock"), nil, 0644); err != nil {
					t.Fatal(err)
				}
			case "discovery":
				if err := os.MkdirAll(filepath.Join(store, "runtime/service"), 0700); err != nil {
					t.Fatal(err)
				}
				// Presence alone denies adoption; no discovery body is consumed.
				if err := os.WriteFile(filepath.Join(store, "runtime/service/discovery.json"), nil, 0000); err != nil {
					t.Fatal(err)
				}
			case "runtime-symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(store, "runtime")); err != nil {
					t.Fatal(err)
				}
			case "native-home-shared":
				if err := os.Mkdir(filepath.Join(store, ".native-home"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if eligible, reason := ProbeOwnedStore("node-test", store); eligible || reason == "" || strings.Contains(reason, store) {
				t.Fatal("unsafe owned Store admitted or private path exposed", eligible, reason)
			}
		})
	}
}

func TestOwnedStoreProbeRejectsHeldControllerAndAllowsReleasedLock(t *testing.T) {
	store := ownedStoreProbeFixture(t)
	lock, err := os.OpenFile(filepath.Join(store, ".caelis-bot-node.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if eligible, reason := ProbeOwnedStore("node-test", store); eligible || reason != "owned-store-controller-busy" {
		t.Fatal("held native controller lock adopted", eligible, reason)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if eligible, reason := ProbeOwnedStore("node-test", store); !eligible || reason != "" {
		t.Fatal("released lock falsely blocks ownership", eligible, reason)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal("probe retained controller lock", err)
	}
	entries, err := os.ReadDir(store)
	if err != nil || len(entries) != 2 {
		t.Fatal("read-only lock probe initialized Store state", entries, err)
	}
}
