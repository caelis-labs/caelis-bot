package tasks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type copyingOutputFixture struct {
	*fixtureRuntime
	offline bool
}

func (f *copyingOutputFixture) ReadWorkFile(_ context.Context, id, ref string) (api.WorkFileContent, error) {
	if f.offline {
		return api.WorkFileContent{}, errors.New("original_owner_offline")
	}
	if _, ok := f.states[id]; !ok || ref != "original-file" {
		return api.WorkFileContent{}, errors.New("not_native_owned")
	}
	return api.WorkFileContent{Name: "data.csv", Data: []byte("name,value\nfixture,42\n")}, nil
}
func TestTaskCopiesRequireProductOwnershipAfterRestartAndOffline(t *testing.T) {
	f := &copyingOutputFixture{fixtureRuntime: newRuntime()}
	root := t.TempDir()
	m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	task := start(t, m, "copy-result")
	first, err := m.MaterializeTaskFile(t.Context(), task.ID, "original-file", true)
	if err != nil {
		t.Fatal(err)
	}
	f.offline = true
	reopened, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	v, err := reopened.MaterializeTaskFile(t.Context(), task.ID, api.TaskArtifactID(task.ID, "original-file"), true)
	if err != nil || !v.Cached || v.Path != first.Path || v.SHA256 != first.SHA256 {
		t.Fatal("original cached result lost", v, err)
	}
	if b, err := os.ReadFile(v.Path); err != nil || string(b) != "name,value\nfixture,42\n" {
		t.Fatal("A local copy is not readable", err)
	}
	if _, err = reopened.MaterializeTaskFile(t.Context(), "unowned", api.TaskArtifactID(task.ID, "original-file"), false); err == nil {
		t.Fatal("cache bypassed task ownership")
	}
	if _, err = reopened.MaterializeTaskFile(t.Context(), task.ID, api.TaskArtifactID("other", "original-file"), false); err == nil {
		t.Fatal("cross-task native reference")
	}
}
func TestInvalidStartFilesAreRejectedWithoutSettlingOriginalUnknown(t *testing.T) {
	f := newRuntime()
	root := t.TempDir()
	m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	m.ConfigureInputResolver(func(context.Context, string) (api.WorkFileContent, error) {
		return api.WorkFileContent{}, errors.New("unknown_selected_upload")
	})
	in := api.TaskStart{RequestID: "file-validation-op", Prompt: "fixture", Attachments: []api.TaskFileReference{{ID: "forged-ref"}}}
	v, err := m.StartTask(t.Context(), in)
	var rejected *api.WorkMessageRejection
	if !errors.As(err, &rejected) || v.Outcome != "rejected" || f.starts != 0 || len(m.ListTasks()) != 0 {
		t.Fatal("file validation pretended unknown or dispatched", v, err)
	}
	f.unknownStart = true
	in.Attachments = nil
	v, err = m.StartTask(t.Context(), in)
	if err == nil || v.Outcome != "unknown" || f.starts != 1 {
		t.Fatal("fixture did not dispatch uncertain original", v, err)
	}
	in.Attachments = []api.TaskFileReference{{ID: "forged-ref"}}
	v, err = m.StartTask(t.Context(), in)
	if err == nil || errors.As(err, &rejected) || v.Outcome != "unknown" || f.starts != 1 {
		t.Fatal("later validation settled or replayed original unknown", v, err)
	}
}
