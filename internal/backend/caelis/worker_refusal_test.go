package caelis

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestNativeWorkerStartPreservesRefusal(t *testing.T) {
	for _, stage := range []string{"create", "prompt"} {
		for _, outcome := range []wire.Outcome{"conflicted", "rejected"} {
			t.Run(stage+"/"+string(outcome), func(t *testing.T) {
				var posts atomic.Int32
				s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/work/state") {
						writeFixture(w, wire.SessionState{SessionId: "work"})
						return
					}
					if r.Method != "POST" {
						t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
						return
					}
					posts.Add(1)
					result := wire.CommandResult{OperationId: r.Header.Get("Idempotency-Key"), Outcome: outcome, ErrorCode: "failed_precondition", ErrorKind: "session_closed", Detail: pointer("synthetic native refusal detail")}
					if stage == "prompt" && strings.HasSuffix(r.URL.Path, "/sessions") {
						result.Outcome, result.SessionId = "committed", pointer("work")
					}
					writeFixture(w, result)
				})
				in := api.WorkStart{ID: "task", Workspace: t.TempDir(), TaskStart: api.TaskStart{RequestID: "start", Title: "Synthetic", Prompt: "synthetic start"}}
				got, err := s.StartWork(authorizedWorkerCall(t.Context(), s), in)
				var refusal *api.WorkMessageRejection
				code := "native_rejected"
				if outcome == "conflicted" {
					code = "native_conflict"
				}
				if !errors.As(err, &refusal) || refusal.Code != code || !strings.Contains(refusal.Cause.Error(), "synthetic native refusal detail") || !strings.Contains(refusal.Cause.Error(), "failed_precondition") || got.Status != "failed" || got.Outcome != string(outcome) {
					t.Fatal("lost native start refusal", got, err)
				}
				op := s.state.Workers[in.ID].PromptID
				if stage == "create" {
					op = s.state.Workers[in.ID].operationID("create", in.ID)
				}
				journalBefore := s.state.Operations[op]
				if journalBefore.Outcome != string(outcome) || journalBefore.Work == nil || journalBefore.Work.RequestID != in.RequestID {
					t.Fatal("lost exact start receipt", journalBefore)
				}
				wantPosts := int32(1)
				if stage == "prompt" {
					wantPosts = 2
					// A later native view cannot overwrite the refused start.
					s.state.Views["work"] = &view{State: wire.SessionState{Run: wire.RunState{Status: pointer("completed")}}}
				}
				for range 2 {
					got, err = s.StartWork(authorizedWorkerCall(t.Context(), s), in)
					if err != nil || got.Status != "failed" || got.Outcome != string(outcome) || posts.Load() != wantPosts {
						t.Fatal("start refusal replayed or reprojected", got, err, posts.Load())
					}
				}
				loaded, err := loadBinding(s.path)
				beforeJSON, _ := json.Marshal(journalBefore)
				afterJSON, _ := json.Marshal(loaded.Operations[op])
				if err != nil || string(afterJSON) != string(beforeJSON) || loaded.Workers[in.ID].Task.Outcome != string(outcome) {
					t.Fatal("durable start receipt changed", err)
				}
			})
		}
	}
}

func TestNativeWorkerContinuationPreservesRefusalWithoutReplay(t *testing.T) {
	for _, steer := range []bool{false, true} {
		for _, outcome := range []wire.Outcome{"conflicted", "rejected"} {
			t.Run(map[bool]string{false: "prompt", true: "steer"}[steer]+"/"+string(outcome), func(t *testing.T) {
				var posts atomic.Int32
				s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "POST" {
						t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
						return
					}
					posts.Add(1)
					writeFixture(w, wire.CommandResult{OperationId: r.Header.Get("Idempotency-Key"), Outcome: outcome, Detail: pointer("synthetic continuation refusal detail")})
				})
				status := "completed"
				if steer {
					status = "working"
				}
				s.state.Workers["task"] = worker{Native: true, Binding: wire.SessionBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: status, Outcome: "accepted"}, PromptID: "previous-start"}
				s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer(status), Active: pointer(steer), HandleId: pointer("h"), RunId: pointer("r"), TurnId: pointer("old-turn")}}, Seen: map[string]bool{}}
				in := api.TaskMessage{ID: "task", RequestID: "refused", Prompt: "synthetic continuation"}
				got, err := s.SendWork(authorizedWorkerCall(t.Context(), s), in)
				var refusal *api.WorkMessageRejection
				code := "native_rejected"
				if outcome == "conflicted" {
					code = "native_conflict"
				}
				if !errors.As(err, &refusal) || refusal.Code != code || !strings.Contains(refusal.Cause.Error(), "synthetic continuation refusal detail") || got.Status != status || got.Outcome != string(outcome) || posts.Load() != 1 {
					t.Fatal("lost native continuation refusal", got, err, posts.Load())
				}
				op := s.state.Workers[in.ID].operationID("send", in.RequestID)
				journalBefore := s.state.Operations[op]
				if journalBefore.Outcome != string(outcome) || journalBefore.Work == nil || journalBefore.Work.RequestID != in.RequestID {
					t.Fatal("lost exact continuation receipt", journalBefore)
				}
				for range 2 {
					got, err = s.SendWork(authorizedWorkerCall(t.Context(), s), in)
					if err != nil || got.Outcome != string(outcome) || posts.Load() != 1 {
						t.Fatal("continuation refusal replayed or folded", got, err, posts.Load())
					}
				}
				loaded, err := loadBinding(s.path)
				beforeJSON, _ := json.Marshal(journalBefore)
				afterJSON, _ := json.Marshal(loaded.Operations[op])
				if err != nil || string(afterJSON) != string(beforeJSON) || loaded.Workers[in.ID].Task.Outcome != string(outcome) {
					t.Fatal("durable continuation receipt changed", err)
				}
			})
		}
	}
}

func TestWorkerKnownStartRefusalIsNotFreshRejection(t *testing.T) {
	for _, stage := range []string{"create", "prompt"} {
		for _, outcome := range []string{"conflicted", "rejected"} {
			t.Run(stage+"/"+outcome, func(t *testing.T) {
				s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Error("known refusal replayed") })
				w := worker{Native: true, Task: api.Task{ID: "task", Workspace: t.TempDir()}, PromptID: "original-prompt", Start: &workerStart{Prompt: "original"}}
				op, path := w.operationID("create", w.Task.ID), "/sessions"
				var request any = wire.CreateSessionRequest{OperationId: &op, Cwd: &w.Task.Workspace, Title: &w.Task.Title, Model: &w.Start.Profile.Model, FastMode: pointer(false)}
				if stage == "prompt" {
					w.Binding.SessionId = "work"
					op, path = w.PromptID, "/sessions/work/prompt"
					request = wire.PromptRequest{OperationId: &op, SessionId: &w.Binding.SessionId, Input: &w.Start.Prompt}
				}
				body, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				s.state.Operations[op] = journal{Path: path, Digest: digest(body), Outcome: outcome}
				got, err := s.advanceWorker(t.Context(), w)
				if err != nil || got.Status != "failed" || got.Outcome != outcome {
					t.Fatal("known refusal became fresh rejection", got, err)
				}
			})
		}
	}
}

func TestWorkerSubmissionTypedRefusalPreservesOutcomeAndCode(t *testing.T) {
	for _, outcome := range []wire.Outcome{"conflicted", "rejected"} {
		t.Run(string(outcome), func(t *testing.T) {
			s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected dispatch") })
			w := worker{Task: api.Task{ID: "task", Status: "completed", Outcome: "accepted"}, PromptID: "previous"}
			w, err := s.beginWorkerSubmission(w, "exact-operation", false)
			if err != nil {
				t.Fatal(err)
			}
			cause := errors.New("synthetic refusal")
			got, err := s.finishWorkerSubmission(w, wire.CommandResult{OperationId: "exact-operation", Outcome: outcome}, cause)
			code := "native_rejected"
			if outcome == "conflicted" {
				code = "native_conflict"
			}
			var refusal *api.WorkMessageRejection
			if got.Outcome != string(outcome) || !errors.As(err, &refusal) || refusal.Code != code || !errors.Is(err, cause) {
				t.Fatal("lost typed native refusal", got, err, refusal)
			}
		})
	}
}

func TestWorkerRecoveredRefusalProjectionPreservesOutcome(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, outcome := range []string{"conflicted", "rejected"} {
			t.Run(map[bool]string{false: "submission", true: "legacy"}[legacy]+"/"+outcome, func(t *testing.T) {
				s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected dispatch") })
				w := worker{Task: api.Task{ID: "task", Status: "unknown", Outcome: "unknown"}, PromptID: "work-send-exact-operation"}
				if !legacy {
					w.Submission = &workerSubmission{OperationID: w.PromptID, BeforeTask: api.Task{ID: "task", Status: "completed", Outcome: "accepted"}}
				}
				s.state.Operations[w.PromptID] = journal{Outcome: outcome}
				if got := s.workerViewLocked(w); got.Outcome != outcome {
					t.Fatal("recovered refusal folded", got)
				}
			})
		}
	}
}
