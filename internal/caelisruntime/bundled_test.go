package caelisruntime

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBundledRuntimeDiscoveryAndUpdateBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("macOS bundle and shell fixture")
	}
	if os.Getenv("BOT_BUNDLE_TEST_CHILD") == "1" {
		bundled := ownBundledPath()
		found, err := Find("")
		if err != nil || found != bundled {
			t.Fatal("bundle must precede PATH", found, err)
		}
		custom := os.Getenv("BOT_BUNDLE_TEST_CUSTOM")
		if found, err = Find(custom); err != nil || found != custom {
			t.Fatal("explicit CLI must win", found, err)
		}
		for _, path := range []string{"", bundled, bundled + "-link"} {
			st, err := Manage(t.Context(), "check-update", path, "")
			if err != nil || !st.Bundled || st.Version != "v0.fixture" || st.UpdateState != "" {
				t.Fatal("bundled inspection", st, err)
			}
			if _, err = Manage(t.Context(), "update", path, ""); err == nil {
				t.Fatal("self-update allowed inside signed app")
			}
		}
		for range 2 {
			if err := PrepareBundled(t.Context(), "", ""); err != nil {
				t.Fatal(err)
			}
		}
		calls, err := os.ReadFile(os.Getenv("BOT_BUNDLE_TEST_LOG"))
		if err != nil || strings.Count(string(calls), "service start") != 1 {
			t.Fatalf("lifecycle requests were not coalesced: %s %v", calls, err)
		}
		if err := PrepareBundled(t.Context(), custom, ""); err != nil {
			t.Fatal(err)
		}
		store, err := Store("")
		home, _ := os.UserHomeDir()
		if err != nil || store != filepath.Join(home, ".caelis") {
			t.Fatal("bundle changed shared Store", store, err)
		}
		if err = os.Remove(bundled); err != nil {
			t.Fatal(err)
		}
		if _, err = Find(""); err == nil {
			t.Fatal("damaged bundle fell back silently to global CLI")
		}
		return
	}
	root := t.TempDir()
	contents := filepath.Join(root, "Test.app", "Contents")
	for _, dir := range []string{"MacOS", "Helpers"} {
		if err := os.MkdirAll(filepath.Join(contents, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	current, _ := os.Executable()
	data, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(contents, "MacOS", "test")
	if err = os.WriteFile(child, data, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := []byte(`#!/bin/sh
case "$1 $2" in
"version --format") printf '{"version":"v0.fixture"}\n';;
"service start") printf '%s\n' "$*" >> "$BOT_BUNDLE_TEST_LOG"; printf '{"state":"running"}\n';;
*) exit 92;;
esac
`)
	bundled := filepath.Join(contents, "Helpers", "caelis")
	custom := filepath.Join(root, "caelis")
	for _, file := range []string{bundled, custom} {
		if err = os.WriteFile(file, fixture, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.Symlink(bundled, bundled+"-link"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), child, "-test.run=^TestBundledRuntimeDiscoveryAndUpdateBoundary$")
	cmd.Env = append(os.Environ(), "BOT_BUNDLE_TEST_CHILD=1", "BOT_BUNDLE_TEST_LOG="+filepath.Join(root, "calls"), "BOT_BUNDLE_TEST_CUSTOM="+custom, "PATH="+root+":/usr/bin:/bin", "HOME="+root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bundle subprocess: %v\n%s", err, output)
	}
}

func TestOrdinaryExecutableHasNoBundleCandidate(t *testing.T) {
	executable, _ := os.Executable()
	if strings.HasSuffix(filepath.Dir(executable), "/Contents/MacOS") {
		t.Skip("already running in an app")
	}
	if path := bundledPath(executable); path != "" {
		t.Fatal("ordinary CLI inferred a bundle", path)
	}
}
