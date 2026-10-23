package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
)

func TestAttachmentStorageIncludesWorkerInputsAndOutputCopies(t *testing.T) {
	a, root := fixtureApp(t, newTestEngine(), Host{})
	selected := filepath.Join(t.TempDir(), "selected.csv")
	if err := os.WriteFile(selected, []byte("name,value\nfixture,1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	inputs, err := workerfiles.OpenInputs(filepath.Join(root, "WorkerInputs"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := inputs.Retain(t.Context(), "original-file-selection", "fixture model input", []api.InputFile{{Name: "selected.csv", Path: selected}}); err != nil {
		t.Fatal(err)
	}
	outputs := workerfiles.OpenOutputs(filepath.Join(root, "Tasks", ".files"))
	if _, err := outputs.Materialize(t.Context(), "retained-task", "original-result", false, func(context.Context) (api.WorkFileContent, error) {
		return api.WorkFileContent{Name: "result.csv", Data: []byte("fixture,result\n1,2\n")}, nil
	}); err != nil {
		t.Fatal(err)
	}
	info, err := a.AttachmentStorage()
	if err != nil || info.Files < 4 || info.Bytes <= 40 {
		t.Fatal("Worker input/output copies are absent from existing attachment storage", info, err)
	}
}

func workerStorageManager(t *testing.T, a *Application, root string) {
	t.Helper()
	m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "fixture", a.machines, a.currentEngine().(api.ReportSubmitter), a.currentEngine().Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	a.configureTaskFiles(m) // Same WorkerInputs/output-reader assembly as normal startup.
	a.mu.Lock()
	a.tasks = m
	a.mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		complete := false
		_, err := backend.WithWorkerFileStorage(t.Context(), a.Backend, filepath.Join(root, "WorkerInputs"), false, nil, func(keep workerfiles.Protection, _ backend.WorkerInputStorage) (api.AttachmentStorage, error) {
			complete = keep.Complete
			return api.AttachmentStorage{}, nil
		})
		if err == nil && complete {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture reference inventory not complete", err)
		}
		time.Sleep(time.Millisecond)
	}
}
func ageStorageFile(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}
func TestExistingAttachmentCleanupCollectsOnlyUnreferencedWorkerCaches(t *testing.T) {
	var moved []string
	bin := t.TempDir()
	var root string
	a, profile := fixtureApp(t, newTestEngine(), Host{TrashFile: func(path string) error {
		rel, err := filepath.Rel(root, path)
		if err != nil || !filepath.IsLocal(rel) {
			t.Fatal("cleanup escaped the fixture profile", path)
		}
		moved = append(moved, path)
		return os.Rename(path, filepath.Join(bin, strconv.Itoa(len(moved))))
	}})
	root = profile
	workerStorageManager(t, a, root)
	store, err := workerfiles.OpenInputs(filepath.Join(root, "WorkerInputs"))
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(t.TempDir(), "original-user-selection")
	if err = os.WriteFile(selected, []byte("original selected bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"accepted-user-input", "unused-rejected-selection"} {
		if _, _, err = store.Retain(t.Context(), id, "input", []api.InputFile{{Name: id + ".csv", Path: selected}}); err != nil {
			t.Fatal(err)
		}
	}
	// Distinct input bytes let the unused selection retire independently.
	if err = os.WriteFile(selected, []byte("different unused bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.Retain(t.Context(), "second-unused-selection", "input", []api.InputFile{{Name: "unused.csv", Path: selected}}); err != nil {
		t.Fatal(err)
	}
	a.Backend.ObserveChat(api.Snapshot{Items: []api.Item{{ID: "accepted-input", RequestID: "accepted-user-input", Kind: "user", Text: "synthetic retained user input"}}})
	_ = filepath.WalkDir(filepath.Join(root, "WorkerInputs"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			ageStorageFile(t, path)
		}
		return err
	})
	outputs := workerfiles.OpenOutputs(filepath.Join(root, "Tasks", ".files"))
	read := func(text string) func(context.Context) (api.WorkFileContent, error) {
		return func(context.Context) (api.WorkFileContent, error) {
			return api.WorkFileContent{Name: "report.csv", Data: []byte(text)}, nil
		}
	}
	first, err := outputs.Materialize(t.Context(), "cached-task", "original-ref", false, read("old output"))
	if err != nil {
		t.Fatal(err)
	}
	ageStorageFile(t, first.Path)
	current, err := outputs.Materialize(t.Context(), "cached-task", "original-ref", true, read("retained offline output"))
	if err != nil {
		t.Fatal(err)
	}
	ageStorageFile(t, current.Path)
	stage := filepath.Join(root, "Tasks", ".inputs", "old-orphan", "input.csv")
	if err = os.MkdirAll(filepath.Dir(stage), 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(stage, []byte("unused stage"), 0600)
	ageStorageFile(t, stage)
	info, err := a.AttachmentStorage()
	if err != nil || !info.CanClean || info.EligibleFiles != 3 {
		t.Fatal("existing usage entry omitted worker garbage/protection", info, err)
	}
	result, err := a.CleanAttachments(t.Context())
	if err != nil || len(moved) != 3 || result.EligibleFiles != 0 {
		t.Fatal("existing cleanup did not retire exact worker garbage", result, moved, err)
	}
	if _, err = os.Stat(current.Path); err != nil {
		t.Fatal("cleanup lost offline current result", err)
	}
	if data, err := os.ReadFile(selected); err != nil || string(data) != "different unused bytes" {
		t.Fatal("cleanup touched user source selection", err)
	}
	if _, err = outputs.Materialize(t.Context(), "cached-task", "original-ref", false, func(context.Context) (api.WorkFileContent, error) {
		return api.WorkFileContent{}, errors.New("B offline")
	}); err != nil {
		t.Fatal("completed cached output no longer available offline", err)
	}
}
func TestAttachmentCleanupUsesExistingBusyGuardBeforeTrash(t *testing.T) {
	calls := 0
	engine := &updateEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{Phase: "unknown", LastReceipt: api.Receipt{ID: "original-unknown", Outcome: "unknown"}}}
	a, _ := fixtureApp(t, engine, Host{TrashFile: func(string) error { calls++; return nil }})
	if _, err := a.CleanAttachments(t.Context()); err == nil || calls != 0 {
		t.Fatal("busy/unknown guard was bypassed", err, calls)
	}
}
