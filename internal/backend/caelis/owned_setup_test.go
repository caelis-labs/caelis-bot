package caelis

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
)

func TestBeginOwnedSetupRefusesBeforeInitialization(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "unmarked"}[existing], func(t *testing.T) {
			parent := t.TempDir()
			store := filepath.Join(parent, "owned-store")
			if existing {
				if err := os.Mkdir(store, 0700); err != nil {
					t.Fatal(err)
				}
			}
			_, err := BeginOwnedSetup(t.Context(), OwnedHostOptions{NodeID: "node-test", Store: store})
			if err == nil || (!codex.OwnedRuntimeSupported() && !errors.Is(err, codex.ErrOwnedRuntimeUnsupported)) {
				t.Fatal("unprepared or unsupported setup was accepted", err)
			}
			if existing {
				entries, err := os.ReadDir(store)
				if err != nil || len(entries) != 0 {
					t.Fatal("unmarked existing Store was adopted or initialized", entries, err)
				}
			} else if _, err := os.Stat(store); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("setup silently prepared Store", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := BeginOwnedSetup(ctx, OwnedHostOptions{})
	if codex.OwnedRuntimeSupported() && !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled Begin lost original error", err)
	}
	if !codex.OwnedRuntimeSupported() && !errors.Is(err, codex.ErrOwnedRuntimeUnsupported) {
		t.Fatal("unsupported build did not refuse before effects", err)
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	shared := filepath.Join(home, ".caelis")
	if _, err := BeginOwnedSetup(t.Context(), OwnedHostOptions{NodeID: "node-test", Store: shared}); err == nil {
		t.Fatal("shared default Store was accepted")
	}
	if _, err := os.Stat(shared); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("shared default Store was initialized", err)
	}
}

func TestSessionOwnedSetupSettingsNeverDelegatesSharedOrClosedHost(t *testing.T) {
	for _, s := range []*Session{{}, {closed: true, owned: &ownedHost{}}} {
		if _, err := s.OwnedSetupSettings(t.Context()); err == nil {
			t.Fatal("unowned or closed Session delegated setup")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (&Session{}).OwnedSetupSettings(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("native setup delegation ignored caller cancellation", err)
	}
}
