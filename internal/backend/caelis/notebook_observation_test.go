package caelis

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeNotebookShellCapabilityAbsentOnPreviousRelease(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_PREVIOUS_SHELL_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_PREVIOUS_SHELL_BINARY to a previous release without Shell observation")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	host := newNativeHostFixture(t, bin)
	notebook := filepath.Join(host.Root, "Notebook")
	if err := os.Mkdir(notebook, 0700); err != nil {
		t.Fatal(err)
	}
	host.start(t, ctx)
	observer := NewNotebookObserver(host.settings, notebook)
	if observer.ShellObservationAvailable(ctx) {
		t.Fatal("the previous release advertised Shell observation")
	}
	if result, err := observer.ObserveShell(ctx, "exit 0"); err == nil || result != nil {
		t.Fatal("an unavailable capability returned an observation")
	}
}
