package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

type inputRefusalRuntime struct {
	*fixtureRuntime
	validation error
}

func (f *inputRefusalRuntime) ValidateWorkFiles(context.Context, string, []api.InputFile) error {
	return f.validation
}

func TestPreDispatchPreparationRefusalsRecordTheirActualStage(t *testing.T) {
	for _, stage := range []string{"reference", "digest", "validation", "workspace"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			f := &inputRefusalRuntime{fixtureRuntime: newRuntime()}
			m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, func() api.Snapshot { return f.snapshot })
			if err != nil {
				t.Fatal(err)
			}
			log := diagnosticlog.New(filepath.Join(root, "Logs"))
			m.ConfigureDiagnostics(log)
			in := api.TaskStart{RequestID: "original-request", Prompt: "PRIVATE_PROMPT"}
			reason := "input_preparation_failed"
			switch stage {
			case "reference":
				in.Attachments = []api.TaskFileReference{{ID: "unknown-reference"}}
			case "digest":
				in.Files = []api.InputFile{{Path: filepath.Join(root, "missing.txt")}}
			case "validation":
				reason = "input_validation_failed"
				f.validation = errors.New("PRIVATE_VALIDATION_CAUSE")
				path := filepath.Join(root, "note.txt")
				if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				in.Files = []api.InputFile{{Path: path, Name: "note.txt"}}
			case "workspace":
				reason = "workspace_preparation_failed"
				if err := os.MkdirAll(m.root, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(m.root, m.startTaskID(in)), []byte("owned collision"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := m.StartTask(t.Context(), in)
			var rejection *api.WorkMessageRejection
			if !errors.As(err, &rejection) || rejection.Code != reason || got.Outcome != "rejected" || f.starts != 0 {
				t.Fatal("wrong refusal or dispatch", got, err, f.starts)
			}
			if stage == "validation" && !errors.Is(err, f.validation) {
				t.Fatal("lost original cause")
			}
			if log.Status()["written"] != uint64(1) {
				t.Fatal("missing or duplicate diagnostic", log.Status())
			}
			raw, err := os.ReadFile(filepath.Join(root, "Logs", "error.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			var record diagnosticlog.Record
			if json.Unmarshal(raw, &record) != nil || record.Code != "start_rejected_before_dispatch" || record.Reason != reason || record.RequestID != in.RequestID || record.TaskID != m.startTaskID(in) {
				t.Fatalf("incomplete diagnostic: %s", raw)
			}
			if strings.Contains(string(raw), "PRIVATE_") {
				t.Fatal("private content leaked")
			}
		})
	}
}
