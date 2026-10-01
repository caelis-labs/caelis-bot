package runtimemanagement

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeArchiveImportVerifiesBeforeSelectionAndWorksWithoutDownload(t *testing.T) {
	manager, archives := fixtureManager(t)
	release, _ := manager.release("codex", "1.0.0")
	if err := manager.ImportArchive(context.Background(), "codex", "1.0.0", bytes.NewReader(archives["codex1.0.0"])); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Manage(context.Background(), Request{Action: "detect", Runtime: "codex"})
	if err != nil || status.Installed {
		t.Fatalf("import selected a runtime: %+v %v", status, err)
	}
	info, err := os.Stat(filepath.Join(manager.directory, archiveCacheName(release)))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("cache is not private: %v %v", info, err)
	}
	manager.fetch = func(context.Context, Release) (io.ReadCloser, error) {
		t.Fatal("verified cache unexpectedly fetched a remote artifact")
		return nil, errors.New("download unavailable")
	}
	install(t, manager, "codex", "1.0.0", "cached-install")
}

func TestNativeArchiveImportRejectsUnreviewedCorruptionAndCancellation(t *testing.T) {
	manager, archives := fixtureManager(t)
	archive := archives["caelis1.0.0"]
	for _, test := range []struct {
		name, version string
		data          []byte
		cancel        bool
	}{
		{"unreviewed", "latest", archive, false},
		{"truncated", "1.0.0", archive[:len(archive)-1], false},
		{"checksum", "1.0.0", bytes.Repeat([]byte{1}, len(archive)), false},
		{"cancelled", "1.0.0", archive, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancel {
				cancel()
			}
			if err := manager.ImportArchive(ctx, "caelis", test.version, bytes.NewReader(test.data)); err == nil {
				t.Fatal("unsafe archive import accepted")
			}
			release, _ := manager.release("caelis", "1.0.0")
			if _, err := os.Stat(filepath.Join(manager.directory, archiveCacheName(release))); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected import published a cache: %v", err)
			}
			entries, _ := os.ReadDir(filepath.Join(manager.directory, "archives"))
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".import-") {
					t.Fatal("failed import retained its partial archive")
				}
			}
		})
	}
}

func TestCacheTamperingAndSymlinkCannotChangeInstalledSelection(t *testing.T) {
	for _, failure := range []string{"digest", "symlink", "directory"} {
		t.Run(failure, func(t *testing.T) {
			manager, archives := fixtureManager(t)
			install(t, manager, "caelis", "1.0.0", "original")
			if err := manager.ImportArchive(context.Background(), "caelis", "2.0.0", bytes.NewReader(archives["caelis2.0.0"])); err != nil {
				t.Fatal(err)
			}
			release, _ := manager.release("caelis", "2.0.0")
			name := filepath.Join(manager.directory, archiveCacheName(release))
			if failure == "digest" {
				if err := os.WriteFile(name, bytes.Repeat([]byte{1}, len(archives["caelis2.0.0"])), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(name); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, archives["caelis2.0.0"], 0600); err != nil {
					t.Fatal(err)
				}
				if failure == "directory" {
					if err := os.Remove(filepath.Dir(name)); err != nil {
						t.Fatal(err)
					}
					outside = filepath.Dir(outside)
					if err := os.WriteFile(filepath.Join(outside, filepath.Base(name)), archives["caelis2.0.0"], 0600); err != nil {
						t.Fatal(err)
					}
					name = filepath.Dir(name)
				}
				if err := os.Symlink(outside, name); err != nil {
					t.Fatal(err)
				}
			}
			status, err := manager.Manage(context.Background(), Request{Action: "update", Runtime: "caelis", Version: "2.0.0", ExpectedVersion: "1.0.0", RequestID: "bad-cache"})
			if err == nil || status.Outcome != "rejected" || status.Version != "1.0.0" {
				t.Fatalf("unsafe cache replaced prior selection: %+v %v", status, err)
			}
			status, err = manager.Manage(context.Background(), Request{Action: "detect", Runtime: "caelis"})
			if err != nil || status.Version != "1.0.0" {
				t.Fatalf("prior selection unavailable: %+v %v", status, err)
			}
		})
	}
}
