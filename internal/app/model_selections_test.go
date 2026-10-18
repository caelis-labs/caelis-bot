package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func awaitSelection(t *testing.T, m *modelSelections, scope, state string) api.ModelSelectionState {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		for _, v := range m.Snapshot() {
			if v.Scope == scope && v.State == state {
				return v
			}
		}
		select {
		case <-deadline:
			t.Fatalf("selection did not reach %s: %+v", state, m.Snapshot())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestModelSelectionAcceptsNewChoiceWhilePreviousApplyIsBlocked(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	m := newModelSelections(t.TempDir(), func(ctx context.Context, scope, runtime string, v api.WorkExecutionSettings) error {
		if v.Model == "first" {
			close(entered)
			<-release
		}
		return nil // Deliberately ignore cancellation to exercise stale completion.
	}, nil)
	defer m.Close()
	if err := m.Save("conversation", "codex", api.WorkExecutionSettings{Model: "first"}); err != nil {
		t.Fatal(err)
	}
	<-entered
	done := make(chan error, 1)
	go func() { done <- m.Save("conversation", "caelis", api.WorkExecutionSettings{Model: "latest"}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("user save waited on Runtime")
	}
	if got := m.Snapshot()[0]; got.Selection.Model != "latest" || got.State != "pending" {
		t.Fatal(got)
	}
	close(release)
	if got := awaitSelection(t, m, "conversation", "applied"); got.Selection.Model != "latest" || got.Revision != 2 {
		t.Fatal(got)
	}
}

func TestModelSelectionRetriesInternallyAndRestoresAfterRestart(t *testing.T) {
	root := t.TempDir()
	m := newModelSelections(root, func(context.Context, string, string, api.WorkExecutionSettings) error {
		return errors.New("private transport diagnostic")
	}, nil)
	if err := m.Save("conversation", "codex", api.WorkExecutionSettings{Model: "saved"}); err != nil {
		t.Fatal(err)
	}
	awaitSelection(t, m, "conversation", "recovering")
	m.Close()
	var attempts atomic.Int32
	next := newModelSelections(root, func(context.Context, string, string, api.WorkExecutionSettings) error {
		if attempts.Add(1) == 1 {
			return errors.New("transient")
		}
		return nil
	}, nil)
	defer next.Close()
	next.Start()
	got := awaitSelection(t, next, "conversation", "applied")
	if got.Selection.Model != "saved" || attempts.Load() != 2 {
		t.Fatal(got, attempts.Load())
	}
	data, _ := os.ReadFile(next.path)
	if string(data) == "" {
		t.Fatal("missing durable selection")
	}
}

func TestModelSelectionStorageFailureKeepsDraftUntilStorageRecovers(t *testing.T) {
	root := t.TempDir()
	var applied atomic.Int32
	m := newModelSelections(root, func(context.Context, string, string, api.WorkExecutionSettings) error { applied.Add(1); return nil }, nil)
	defer m.Close()
	if err := os.Mkdir(m.path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.Save("work", "caelis", api.WorkExecutionSettings{Model: "saved"}); err != nil {
		t.Fatal(err)
	}
	awaitSelection(t, m, "work", "saving")
	if applied.Load() != 0 {
		t.Fatal("applied without durable intent")
	}
	if err := os.Remove(m.path); err != nil {
		t.Fatal(err)
	}
	m.wake <- struct{}{}
	awaitSelection(t, m, "work", "applied")
}

func TestModelSelectionFailureDoesNotPreventOtherScopeOrShutdown(t *testing.T) {
	m := newModelSelections(t.TempDir(), func(ctx context.Context, scope, runtime string, v api.WorkExecutionSettings) error {
		if scope == "conversation" {
			return errors.New("offline")
		}
		return nil
	}, nil)
	if err := m.Save("conversation", "codex", api.WorkExecutionSettings{Model: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Save("work", "conversation", api.WorkExecutionSettings{}); err != nil {
		t.Fatal(err)
	}
	awaitSelection(t, m, "work", "applied")
	m.Close()
	if err := m.Save("work", "conversation", api.WorkExecutionSettings{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestModelSelectionJournalDoesNotAdvertiseInvalidStoredChoice(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "model-selections.json"), []byte(`{"conversation":{"runtime":"other","selection":{"model":"x"},"revision":1,"state":"applied"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	m := newModelSelections(root, nil, nil)
	defer m.Close()
	if len(m.Snapshot()) != 0 {
		t.Fatal(m.Snapshot())
	}
}
