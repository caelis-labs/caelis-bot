package caelis

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

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
