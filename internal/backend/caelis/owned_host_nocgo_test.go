//go:build darwin && !cgo

package caelis

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
)

func TestUnsupportedOwnedCaelisDoesNotPrepareStoreOrLaunch(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "missing-store")
	helper := filepath.Join(root, "helper")
	marker := filepath.Join(root, "launched")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	opts := OwnedHostOptions{NodeID: "unsupported-node", Store: store, Binary: helper, WatchdogHelper: helper}
	for _, existing := range []bool{false, true} {
		host, err := startOwnedHostWithStore(t.Context(), opts, existing)
		if host != nil || !errors.Is(err, codex.ErrOwnedRuntimeUnsupported) {
			t.Fatal(host, err)
		}
	}
	for _, path := range []string{store, marker} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("unsupported ownership produced effects", path, err)
		}
	}
}
