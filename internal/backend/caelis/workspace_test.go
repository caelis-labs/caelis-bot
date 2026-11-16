package caelis

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

func canonicalWorkspace(t *testing.T) string {
	t.Helper()
	path, err := api.ResolveTaskWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRedirectedWorkspaceCannotContinueCaelisWorker(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Error("redirected cwd dispatched") })
	workspace, err := api.ResolveTaskWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.state.Workers["task"] = worker{Native: true, Task: api.Task{ID: "task", Workspace: workspace, Status: "completed", Outcome: "accepted"}, Binding: wire.SessionBinding{SessionId: "work"}, PromptID: "original-start"}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("completed"), Active: pointer(false)}}}
	s.state.Operations["original-start"] = journal{Outcome: "accepted"}
	s.state.Operations["original-unknown"] = journal{Outcome: "unknown"}
	before, _ := json.Marshal(s.state)
	if err := os.Remove(workspace); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), workspace); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err = s.SendWork(authorizedWorkerCall(t.Context(), s), api.TaskMessage{ID: "task", RequestID: "new-continuation", Prompt: "continue"})
	after, _ := json.Marshal(s.state)
	if !errors.Is(err, api.ErrTaskWorkspaceUnavailable) || string(after) != string(before) {
		t.Fatal("redirected binding continued or altered prior receipts", err)
	}
}

func TestWaitingWorkerWorkspaceAdmissionKeepsNativeRouting(t *testing.T) {
	for _, state := range []string{"waiting-flag", "running-status", "waiting-status", "active-steer", "manual-approval"} {
		t.Run(state, func(t *testing.T) {
			head := wire.SessionState{SessionId: "work", Run: wire.RunState{Active: pointer(false), WaitingApproval: pointer(true), Status: pointer("waiting_approval"), HandleId: pointer("h"), RunId: pointer("r"), TurnId: pointer("original-turn")}}
			wantPath := "/api/control/v1/sessions/work/prompt"
			switch state {
			case "waiting-flag":
				// The pinned native gateway can expose durable waiting state
				// without an active handle or caller-resolvable approval.
				head.Run.HandleId, head.Run.TurnId = nil, nil
			case "running-status":
				head.Run.WaitingApproval, head.Run.Status = pointer(false), pointer("running")
			case "waiting-status":
				head.Run.WaitingApproval = pointer(false)
			case "active-steer":
				head.Run.Active = pointer(true)
				wantPath = "/api/control/v1/sessions/work/steer"
			case "manual-approval":
				head.Approval.Active = testApproval()
				wantPath = "/api/control/v1/sessions/work/steer"
			}
			var posts atomic.Int32
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					writeFixture(w, head)
					return
				}
				posts.Add(1)
				if strings.HasSuffix(r.URL.Path, "/cancel") || strings.HasSuffix(r.URL.Path, "/resolve") {
					writeFixture(w, wire.CommandResult{OperationId: r.Header.Get("Idempotency-Key"), Outcome: "committed"})
					return
				}
				if r.URL.Path != wantPath {
					t.Errorf("workspace check changed native routing: %s want %s", r.URL.Path, wantPath)
				}
				outcome := wire.Outcome("accepted")
				if state == "waiting-flag" {
					outcome = "rejected" // Native authority decides whether this retained run can resume.
				}
				writeFixture(w, wire.CommandResult{OperationId: r.Header.Get("Idempotency-Key"), Outcome: outcome})
			})
			workspace := t.TempDir()
			s.state.Workers["task"] = worker{Native: true, Binding: wire.SessionBinding{SessionId: "work"}, Task: api.Task{ID: "task", Workspace: workspace, Status: "working", Outcome: "accepted"}, PromptID: "original-start"}
			s.state.Views["work"] = &view{State: head, Seen: map[string]bool{}}
			if err := os.Remove(workspace); err != nil {
				t.Fatal(err)
			}
			if got, err := s.ReadWork(t.Context(), "task"); err != nil || got.Workspace != workspace {
				t.Fatal("directory loss blocked original read", got, err)
			}
			if state != "manual-approval" && len(s.Snapshot().Approvals) != 0 {
				t.Fatal("waiting fact invented manual approval")
			}
			priorPosts := int32(0)
			if state == "manual-approval" {
				approvals := s.Snapshot().Approvals
				if len(approvals) != 1 {
					t.Fatal("original manual approval lost", approvals)
				}
				if err := s.DecideWork(t.Context(), api.WorkDecision{TaskID: "task", RequestID: "original-decision", Decision: api.Decision{ID: approvals[0].ID, Choice: "native-allow"}}); err != nil {
					t.Fatal("directory loss blocked original approval", err)
				}
				priorPosts++
			}
			got, err := s.SendWork(authorizedWorkerCall(t.Context(), s), api.TaskMessage{ID: "task", RequestID: "original-input", Prompt: "continue original goal"})
			wantOutcome := "accepted"
			if state == "waiting-flag" {
				wantOutcome = "rejected"
			}
			if errors.Is(err, api.ErrTaskWorkspaceUnavailable) || got.Outcome != wantOutcome || posts.Load() != priorPosts+1 || state != "waiting-flag" && err != nil {
				t.Fatal("active native input was blocked by cwd check", got, err, posts.Load())
			}
			_, err = s.StopWork(t.Context(), "task")
			if value(head.Run.Active) {
				if err != nil || posts.Load() != priorPosts+2 {
					t.Fatal("directory loss blocked original stop", err)
				}
			} else if err == nil || errors.Is(err, api.ErrTaskWorkspaceUnavailable) || posts.Load() != priorPosts+1 {
				t.Fatal("no native active handle must retain its original stop refusal", err)
			}
		})
	}
}

func TestDeletedWorkspaceBlocksOnlyFreshIdleContinuation(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "active"}[busy], func(t *testing.T) {
			var posts atomic.Int32
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					return
				}
				posts.Add(1)
				writeFixture(w, wire.CommandResult{OperationId: r.Header.Get("Idempotency-Key"), Outcome: "accepted"})
			})
			workspace := t.TempDir()
			status := "completed"
			if busy {
				status = "working"
			}
			s.state.Workers["task"] = worker{Native: true, Binding: wire.SessionBinding{SessionId: "work"}, Task: api.Task{ID: "task", Workspace: workspace, Status: status, Outcome: "accepted"}, PromptID: "previous-start"}
			s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer(status), Active: pointer(busy), HandleId: pointer("h"), RunId: pointer("r"), TurnId: pointer("original-turn")}}, Seen: map[string]bool{}}
			root := t.TempDir()
			m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", s, s, s.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(workspace); err != nil {
				t.Fatal(err)
			}
			in := api.TaskMessage{ID: "task", RequestID: "workspace-continue", Prompt: "continue"}
			got, err := m.SendTask(authorizedWorkerCall(t.Context(), s), in)
			if busy {
				if err != nil || got.Outcome != "accepted" || posts.Load() != 1 {
					t.Fatal("active steering blocked", got, err, posts.Load())
				}
				// Reading the original accepted operation must not replay it or
				// reclassify it because its directory has disappeared.
				if got, err = m.SendTask(authorizedWorkerCall(t.Context(), s), in); err != nil || got.Outcome != "accepted" || posts.Load() != 1 {
					t.Fatal("accepted receipt changed", got, err, posts.Load())
				}
			} else {
				var rejected *api.WorkMessageRejection
				if got.Outcome != "rejected" || !errors.As(err, &rejected) || rejected.Code != "workspace_unavailable" || !errors.Is(err, api.ErrTaskWorkspaceUnavailable) || posts.Load() != 0 {
					t.Fatal("missing workspace reached native dispatch", got, err, posts.Load())
				}
				if len(s.state.Operations) != 0 || s.state.Workers["task"].Submission != nil {
					t.Fatal("preflight prepared a native submission")
				}
			}
			if s.state.Workers["task"].Task.Workspace != workspace {
				t.Fatal("rebound original workspace")
			}
			if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("recreated original workspace", err)
			}
		})
	}
}
