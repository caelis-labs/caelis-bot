package codex

import (
	"errors"
	"os"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestDeletedWorkspaceRejectsIdleContinuationWithoutChangingBinding(t *testing.T) {
	for _, replacement := range []string{"deleted", "file", "symlink"} {
		t.Run(replacement, func(t *testing.T) {
			s, f, d, m := taskPair(t)
			sendSynthetic(t, s, "workspace-parent")
			task := newTask(t, m, "workspace-create")
			finishTask(s, f, task.ID, "completed")
			if _, err := m.ReadTask(testContext(t), task.ID); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(task.Workspace); err != nil {
				t.Fatal(err)
			}
			if replacement == "file" {
				if err := os.WriteFile(task.Workspace, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if replacement == "symlink" {
				if err := os.Symlink(t.TempDir(), task.Workspace); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			in := api.TaskMessage{ID: task.ID, RequestID: "workspace-continue", Prompt: "continue"}
			got, err := m.SendTask(testContext(t), in)
			var rejected *api.WorkMessageRejection
			if got.Outcome != "rejected" || !errors.As(err, &rejected) || rejected.Code != "workspace_unavailable" || !errors.Is(err, api.ErrTaskWorkspaceUnavailable) {
				t.Fatal("missing definitive workspace rejection", got, err)
			}
			f.mu.Lock()
			if d.sends != 1 || d.resumed != nil {
				t.Error("invalid cwd reached native continuation", d.sends, d.resumed)
			}
			f.mu.Unlock()
			s.mu.Lock()
			record := s.binding.Tasks[task.ID]
			_, recorded := record.Requests[in.RequestID]
			if recorded || record.Pending != "" || record.View.Workspace != task.Workspace || record.View.Status != "completed" {
				t.Error("preflight changed original binding or prepared a dispatch", record)
			}
			s.mu.Unlock()
			if got, err = m.ReadTask(testContext(t), task.ID); err != nil || got.Status != "completed" || got.Workspace != task.Workspace {
				t.Fatal("history no longer readable", got, err)
			}
			// The accepted creation receipt remains authoritative after deletion.
			if got, err = s.SendWork(testContext(t), api.TaskMessage{ID: task.ID, RequestID: "workspace-create", Prompt: "Make a synthetic artifact in your workspace; no external writes."}); err != nil || got.Outcome != "accepted" {
				t.Fatal("accepted receipt was reclassified", got, err)
			}
		})
	}
}

func TestDeletedWorkspaceDoesNotInterruptActiveWorker(t *testing.T) {
	s, _, _, m := taskPair(t)
	sendSynthetic(t, s, "active-workspace-parent")
	task := newTask(t, m, "active-workspace-create")
	if err := os.RemoveAll(task.Workspace); err != nil {
		t.Fatal(err)
	}
	if got, err := m.ReadTask(testContext(t), task.ID); err != nil || got.Status != "working" {
		t.Fatal("active task changed", got, err)
	}
	if got, err := m.SendTask(testContext(t), api.TaskMessage{ID: task.ID, RequestID: "active-workspace-steer", Prompt: "keep the same goal"}); err != nil || got.Outcome != "accepted" {
		t.Fatal("active steering blocked", got, err)
	}
	if _, err := m.StopTask(testContext(t), task.ID); err != nil {
		t.Fatal("stop blocked by missing workspace", err)
	}
}
