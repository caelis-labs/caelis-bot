package bot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type taskCopyToolFixture struct {
	api.TaskProvider
	copy      api.TaskFileCopy
	task, ref string
}

func (f *taskCopyToolFixture) MaterializeTaskFile(_ context.Context, task, ref string, refresh bool) (api.TaskFileCopy, error) {
	f.task, f.ref = task, ref
	return f.copy, nil
}
func TestTaskFileToolProvidesActualReadableALocalCopy(t *testing.T) {
	r, _, _ := fixture(t)
	path := filepath.Join(t.TempDir(), "result.csv")
	if err := os.WriteFile(path, []byte("name,value\nfixture,42\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f := &taskCopyToolFixture{copy: api.TaskFileCopy{ID: api.TaskArtifactID("owned-task", "native-file"), Name: "result.csv", Path: path, SHA256: "checked-native-bytes", Bytes: 22, Cached: true, FetchedAt: 12345}}
	if err := r.ConfigureTasks(f, nil); err != nil {
		t.Fatal(err)
	}
	out := r.callTasksV3(t.Context(), map[string]any{"type": "file", "task": "owned-task", "file": "native-file"})
	if out.IsError || f.task != "owned-task" || f.ref != "native-file" {
		t.Fatal(out)
	}
	data := out.StructuredContent["data"].(map[string]any)
	actual, err := os.ReadFile(data["localPath"].(string))
	if err != nil || string(actual) != "name,value\nfixture,42\n" || data["cached"] != true || data["sha256"] != f.copy.SHA256 {
		t.Fatal("Agent received only a remote handle, not readable bytes", data, err)
	}
}
