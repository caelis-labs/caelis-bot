//go:build (!darwin || !cgo) && !linux

package codex

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUnsupportedOwnedRuntimeRefusesBeforeHelperOrState(t *testing.T) {
	if OwnedRuntimeSupported() {
		t.Fatal("unsupported build advertised ownership")
	}
	root := t.TempDir()
	directory := filepath.Join(root, "runtime")
	// Executable paths deliberately exist: refusal cannot rely on a missing helper.
	helper := filepath.Join(root, "helper")
	marker := filepath.Join(root, "launched")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if process, err := StartSupervisedProcess(t.Context(), SupervisedProcessOptions{HelperPath: helper, Binary: helper, Directory: directory, Kind: SupervisedCodexStdio}); process != nil || !errors.Is(err, ErrOwnedRuntimeUnsupported) {
		t.Fatal(process, err)
	}
	if foreground, err := StartOwnedForeground(t.Context(), helper, helper, directory, filepath.Join(root, "store")); foreground != nil || !errors.Is(err, ErrOwnedRuntimeUnsupported) {
		t.Fatal(foreground, err)
	}
	session := NewSession(SessionOptions{ForceOwned: true, Binary: helper, WatchdogHelper: helper, Directory: directory})
	if err := session.Connect(t.Context()); !errors.Is(err, ErrOwnedRuntimeUnsupported) {
		t.Fatal(err)
	}
	worker := &WorkerClient{lease: &workerLeaseFence{}}
	if err := worker.Connect(t.Context()); !errors.Is(err, ErrOwnedRuntimeUnsupported) {
		t.Fatal(err)
	}
	if err := RunSupervisedRuntime(t.Context(), nil); !errors.Is(err, ErrOwnedRuntimeUnsupported) {
		t.Fatal(err)
	}
	if err := session.VerifyOwnedSupervisor(t.Context()); !errors.Is(err, ErrOwnedRuntimeUnsupported) {
		t.Fatal(err)
	}
	if err := session.ConfigureOwnedDeadline(t.Context(), "epoch", time.Now().Add(40*time.Second)); !errors.Is(err, ErrOwnedRuntimeUnsupported) {
		t.Fatal(err)
	}
	if _, err := ProbeSupervisedAuth(t.Context(), helper, helper, directory); !errors.Is(err, ErrOwnedRuntimeUnsupported) {
		t.Fatal(err)
	}
	for _, path := range []string{directory, marker, filepath.Join(root, "store")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("unsupported ownership produced effects", path, err)
		}
	}
}
