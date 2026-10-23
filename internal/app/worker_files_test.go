package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

type inputReferenceEngine struct {
	api.Engine
	snapshot api.Snapshot
	path     string
	resolver int
	input    api.Submission
}

type inputWorkerFixture struct {
	*workerFixture
	inputs [][]byte
	names  []string
}

func (w *inputWorkerFixture) receive(files []api.InputFile) error {
	for _, file := range files {
		data, err := os.ReadFile(file.Path)
		if err != nil {
			return err
		}
		w.inputs = append(w.inputs, data)
		w.names = append(w.names, file.Name)
	}
	return nil
}
func (w *inputWorkerFixture) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	if err := w.receive(in.Files); err != nil {
		return api.Task{}, err
	}
	return w.workerFixture.StartWork(ctx, in)
}
func (w *inputWorkerFixture) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	if err := w.receive(in.Files); err != nil {
		return api.Task{}, err
	}
	return w.workerFixture.SendWork(ctx, in)
}

func (e *inputReferenceEngine) Snapshot() api.Snapshot { return e.snapshot }
func (e *inputReferenceEngine) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	e.input = in
	return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
}
func (e *inputReferenceEngine) Artifact(string) (string, error) {
	e.resolver++
	return e.path, nil
}

func TestWorkerUploadReferenceResolvesAfterDraftRemovalAndResidentSwitch(t *testing.T) {
	root := t.TempDir()
	engine := &inputReferenceEngine{}
	back := backend.NewService(engine, nil, nil, nil, nil)
	a := &Application{root: root, Backend: back, engine: engine}
	work := &inputWorkerFixture{workerFixture: &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}}
	m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", work, work, engine.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	a.configureTaskFiles(m)
	selected := filepath.Join(t.TempDir(), "source.csv")
	data := []byte("name,value\r\n甲,7\r\n")
	if err := os.WriteFile(selected, data, 0600); err != nil {
		t.Fatal(err)
	}
	receipt, err := backend.SubmitRemote(t.Context(), back, api.Submission{ID: "user-original-material", Text: "use the selected CSV"}, []api.InputFile{{Name: "source.csv", Path: selected}})
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	start := strings.Index(engine.input.ModelInputOverride, "upload-file:")
	if start < 0 {
		t.Fatal("resident did not receive a delegatable upload reference")
	}
	id := strings.Split(engine.input.ModelInputOverride[start:], `"`)[0]
	if err := os.Remove(selected); err != nil {
		t.Fatal(err)
	}
	replacement := &inputReferenceEngine{}
	a.setEngine(replacement)
	v, err := a.residentWorkerFile(t.Context(), id)
	if err != nil || v.Name != "source.csv" || !bytes.Equal(v.Data, data) || replacement.resolver != 0 {
		t.Fatal("retained user upload was tied to the old resident", v, err)
	}
	if _, err := a.residentWorkerFile(t.Context(), "upload-file:forged"); err == nil || replacement.resolver != 0 {
		t.Fatal("invalid upload ID fell through to native artifacts", err)
	}
	if _, err := a.residentWorkerFile(t.Context(), selected); err == nil || replacement.resolver != 0 {
		t.Fatal("local path became an upload reference", err)
	}
	task, err := m.StartTask(t.Context(), api.TaskStart{RequestID: "create-from-user-upload", Prompt: "read original CSV", Attachments: []api.TaskFileReference{{ID: id}}})
	if err != nil || len(work.inputs) != 1 || work.names[0] != "source.csv" || !bytes.Equal(work.inputs[0], data) {
		t.Fatal("retained upload did not reach shared Worker create", task, err)
	}
	if _, err := m.SendTask(t.Context(), api.TaskMessage{ID: task.ID, RequestID: "continue-from-user-upload", Prompt: "check the same original material", Attachments: []api.TaskFileReference{{ID: id}}}); err != nil || len(work.inputs) != 2 || !bytes.Equal(work.inputs[1], data) {
		t.Fatal("retained upload did not reach original Worker continue", err)
	}
}

func TestResidentArtifactInputsKeepTheirOriginalOwnershipCheck(t *testing.T) {
	file := filepath.Join(t.TempDir(), "result.txt")
	if err := os.WriteFile(file, []byte("original artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := &inputReferenceEngine{path: file, snapshot: api.Snapshot{Items: []api.Item{{Artifacts: []api.Artifact{{ID: "owned-native-artifact", Name: "result.txt"}}}}}}
	a := &Application{Backend: backend.NewService(engine, nil, nil, nil, nil), engine: engine}
	if _, err := a.residentWorkerFile(t.Context(), "unowned-native-artifact"); err == nil || engine.resolver != 0 {
		t.Fatal("unowned artifact reached native resolver", err)
	}
	v, err := a.residentWorkerFile(t.Context(), "owned-native-artifact")
	if err != nil || string(v.Data) != "original artifact" || engine.resolver != 1 {
		t.Fatal("existing owned artifact route changed", v, err)
	}
}
