package bot

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
	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

type delegateAdmissionRuntime struct {
	api.WorkRuntime
	states       []api.WorkState
	admit        error
	nativeReason string
	starts       int
}

func (f *delegateAdmissionRuntime) WorkAdmission(context.Context) error { return f.admit }
func (f *delegateAdmissionRuntime) WorkStates() []api.WorkState         { return f.states }
func (f *delegateAdmissionRuntime) StartWork(_ context.Context, in api.WorkStart) (api.Task, error) {
	f.starts++
	v := api.Task{ID: in.ID, Title: in.Title, Workspace: in.Workspace, Status: "working", Outcome: "accepted"}
	if f.nativeReason != "" {
		v.Status, v.Outcome = "failed", "rejected"
		if f.nativeReason == "native_conflict" {
			v.Outcome = "conflicted"
		}
		return v, &api.WorkMessageRejection{Code: f.nativeReason, Cause: errors.New("native assignment refused")}
	}
	f.states = append(f.states, api.WorkState{Task: v, Activity: "active"})
	return v, nil
}
func (f *delegateAdmissionRuntime) SubmitReport(context.Context, api.Submission) (api.Receipt, error) {
	return api.Receipt{}, errors.New("unexpected report")
}

func TestDelegateAdmissionSourcesAndDiagnostic(t *testing.T) {
	for _, reason := range []string{"updater_paused", "capacity_full", "work_admission_failed", "workspace_invalid", "native_conflict", "native_rejected"} {
		t.Run(reason, func(t *testing.T) {
			r, _, _ := fixture(t)
			root := t.TempDir()
			f := &delegateAdmissionRuntime{}
			m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, func() api.Snapshot { return api.Snapshot{CanSend: true} })
			if err != nil {
				t.Fatal(err)
			}
			m.SetLocale(func() i18n.Locale { return i18n.Locale("en") })
			m.ConfigureDiagnostics(diagnosticlog.New(filepath.Join(root, "Logs")))
			if err := r.ConfigureTasks(m, nil); err != nil {
				t.Fatal(err)
			}
			workspace, wantCause := root, ""
			switch reason {
			case "updater_paused":
				if err := m.PauseForUpdate(func() error { return nil }); err != nil {
					t.Fatal(err)
				}
				wantCause = "Installing updates. Try again later."
			case "capacity_full":
				m.ConfigureLimit(func() int { return 1 })
				if _, err := m.StartTask(t.Context(), api.TaskStart{RequestID: "occupied", Prompt: "Existing assignment", Workspace: root}); err != nil {
					t.Fatal(err)
				}
				wantCause = "The running-task limit is reached."
			case "work_admission_failed":
				f.admit = errors.New("native admission unavailable")
				wantCause = f.admit.Error()
			case "workspace_invalid":
				workspace = filepath.Join(root, "absent")
				_, cause := api.ResolveTaskWorkspace(workspace)
				wantCause = cause.Error()
			default:
				f.nativeReason = reason
				wantCause = "native assignment refused"
			}
			startsBefore := f.starts
			invocation := api.ToolInvocation{Provider: "codex", Session: "resident", Turn: "turn", CallID: reason}
			ctx := api.WithToolInvocation(t.Context(), invocation)
			args, _ := json.Marshal(map[string]string{"title": "Review", "prompt": "private assignment text", "workspace": workspace})
			out := r.CallTool(ctx, "bot_delegate", args)
			if !out.IsError {
				t.Fatal("refusal became success", out)
			}
			failure := out.StructuredContent["error"].(map[string]string)
			data := out.StructuredContent["data"].(map[string]any)
			if failure["reason"] != reason || !strings.Contains(failure["detail"], wantCause) || data["requestId"] != toolSubmissionID(invocation) {
				t.Fatalf("lost classification, cause or original identity: %#v", out.StructuredContent)
			}
			temporary := reason == "updater_paused" || reason == "capacity_full" || reason == "work_admission_failed"
			message := strings.ToLower(failure["message"])
			if temporary != strings.Contains(message, "retry") || strings.Contains(message, "definitively rejected") {
				t.Fatal("wrong retry guidance", failure)
			}
			if reason != "workspace_invalid" && strings.Contains(message, "check the selected workspace") {
				t.Fatal("misleading path guidance", failure)
			}
			if strings.HasPrefix(reason, "native_") {
				if f.starts != startsBefore+1 {
					t.Fatal("missing native refusal")
				}
				return
			}
			if f.starts != startsBefore {
				t.Fatal("pre-dispatch refusal dispatched")
			}
			logs, err := os.ReadFile(filepath.Join(root, "Logs", "error.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(logs)), "\n")
			var record diagnosticlog.Record
			if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &record) != nil {
				t.Fatalf("expected one diagnostic: %s", logs)
			}
			if record.Level != "warning" || record.Code != "start_rejected_before_dispatch" || record.TaskID == "" || record.RequestID != toolSubmissionID(invocation) || record.Reason != reason || record.Workspace != workspace {
				t.Fatalf("incomplete diagnostic: %+v", record)
			}
			if strings.Contains(string(logs), "private assignment text") {
				t.Fatal("prompt leaked into diagnostic")
			}
		})
	}
}
