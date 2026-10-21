package caelis

import (
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

func TestManagerCaelisContinuationKeepsDefiniteRejection(t *testing.T) {
	for _, predispatch := range []bool{true, false} {
		t.Run(map[bool]string{true: "missing steering target", false: "native HTTP refusal"}[predispatch], func(t *testing.T) {
			var posts atomic.Int32
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					return
				}
				if posts.Add(1) == 1 && !predispatch {
					w.WriteHeader(http.StatusForbidden)
					writeFixture(w, map[string]string{"code": "permission_denied"})
					return
				}
				writeFixture(w, wire.CommandResult{OperationId: r.Header.Get("Idempotency-Key"), Outcome: "accepted"})
			})
			s.state.Workers["task"] = worker{Native: true, Binding: wire.SessionBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "completed", Outcome: "accepted"}, PromptID: "previous-start"}
			s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("completed"), Active: pointer(false), TurnId: pointer("old-turn")}}, Seen: map[string]bool{}}
			if predispatch {
				s.state.Views["work"].State.Run = wire.RunState{Status: pointer("running"), Active: pointer(true)}
			}
			root := t.TempDir()
			m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", s, s, s.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			in := api.TaskMessage{ID: "task", RequestID: "definitely-refused", Prompt: "synthetic continuation"}
			got, err := m.SendTask(authorizedWorkerCall(t.Context(), s), in)
			var rejection *api.WorkMessageRejection
			wantPosts := int32(1)
			if predispatch {
				wantPosts = 0
			}
			if got.Outcome != "rejected" || !errors.As(err, &rejection) || posts.Load() != wantPosts {
				t.Fatalf("lost invocation evidence: %+v %v posts=%d", got, err, posts.Load())
			}
			op := "work-send-" + digest([]byte(in.RequestID))
			journal, exists := s.state.Operations[op]
			if predispatch && exists || !predispatch && (!exists || journal.Outcome != "rejected") {
				t.Fatal("wrong original journal", exists, journal.Outcome)
			}
			// Repairing the target does not replay this ID. The manager preserves the
			// rejected outcome even when no native journal could have been created.
			s.state.Views["work"].State.Run = wire.RunState{Status: pointer("running"), Active: pointer(true), HandleId: pointer("h"), RunId: pointer("r"), TurnId: pointer("original-turn")}
			got, err = m.SendTask(authorizedWorkerCall(t.Context(), s), in)
			if err != nil || got.Outcome != "rejected" || posts.Load() != wantPosts {
				t.Fatal("original refusal replayed/lost", got, err, posts.Load())
			}
			m, err = tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", s, s, s.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			got, err = m.SendTask(authorizedWorkerCall(t.Context(), s), in)
			if err != nil || got.Outcome != "rejected" || posts.Load() != wantPosts {
				t.Fatal("restart lost original refusal", got, err)
			}
			in.RequestID = "explicit-new-attempt"
			got, err = m.SendTask(authorizedWorkerCall(t.Context(), s), in)
			if err != nil || got.Outcome != "accepted" || posts.Load() != wantPosts+1 {
				t.Fatal("new explicit attempt blocked", got, err, posts.Load())
			}
		})
	}
}

func TestManagerCaelisUnknownContinuationCannotBecomeRejectionOnLookupFailure(t *testing.T) {
	var posts, reads atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			drop(w)
			return
		}
		reads.Add(1)
		w.WriteHeader(http.StatusForbidden)
		writeFixture(w, map[string]string{"code": "permission_denied"})
	})
	s.state.Workers["task"] = worker{Native: true, Binding: wire.SessionBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "working", Outcome: "accepted"}, PromptID: "previous-start"}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("running"), Active: pointer(true), HandleId: pointer("h"), RunId: pointer("r"), TurnId: pointer("t")}}, Seen: map[string]bool{}}
	root := t.TempDir()
	m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", s, s, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	in := api.TaskMessage{ID: "task", RequestID: "unknown-original", Prompt: "synthetic continuation"}
	got, err := m.SendTask(authorizedWorkerCall(t.Context(), s), in)
	if got.Outcome != "unknown" || err == nil || posts.Load() != 1 {
		t.Fatal("missing unknown", got, err)
	}
	op := "work-send-" + digest([]byte(in.RequestID))
	if err = s.recoverOperation(t.Context(), op); err == nil || reads.Load() != 1 {
		t.Fatal("missing failed read", err, reads.Load())
	}
	got, err = m.SendTask(authorizedWorkerCall(t.Context(), s), in)
	var rejection *api.WorkMessageRejection
	if got.Outcome != "unknown" || errors.As(err, &rejection) || posts.Load() != 1 || s.state.Operations[op].Outcome != "unknown" {
		t.Fatal("read rejection settled/replayed unknown", got, err, posts.Load())
	}
}
