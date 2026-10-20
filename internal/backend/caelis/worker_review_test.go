package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestWorkerApprovalRefreshWithHealthyStream(t *testing.T) {
	for _, settled := range []bool{false, true} {
		t.Run(fmt.Sprint(settled), func(t *testing.T) {
			head := wire.SessionState{SessionId: "work", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", Run: wire.RunState{Active: pointer(true), Status: pointer("running")}}
			if settled {
				head.Approval.Active = testApproval()
			}
			var reads atomic.Int32
			var s *Session
			s = fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Errorf("approval observation dispatched %s", r.Method)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/state") {
					reads.Add(1)
					current := clone(head)
					if settled {
						current.Approval.Active = nil
						current.Run.Active, current.Run.Status = pointer(false), pointer("completed")
					} else {
						current.Approval.Active = testApproval()
					}
					s.mu.Lock()
					s.state.Views["work"].Observed++ // Concurrent prose must not starve approval reconciliation.
					s.mu.Unlock()
					writeFixture(w, current)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				send := func(event string, value any) {
					raw, _ := json.Marshal(value)
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
				}
				send("caelis.control.bootstrap", head)
				e := wire.Envelope{Kind: "session/request_permission", EventId: pointer("permission"), ApprovalRequestId: pointer("approval"), Permission: &wire.RequestPermission{SessionId: "work", ToolCall: &wire.ACPToolCallUpdate{ToolCallId: "call", Kind: pointer("execute")}}}
				if settled {
					e.Kind, e.Permission = "caelis/lifecycle", nil
					e.Lifecycle = &wire.LifecycleEvent{State: "completed"}
				}
				send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", NextCursor: pointer("cursor"), Events: []wire.Envelope{e}})
			})
			s.state.Workers["task"] = worker{Binding: wire.SessionBinding{SessionId: "work", PrincipalId: "owner"}, Task: api.Task{ID: "task", Status: "working"}}
			for i := range 3 {
				s.takeObservationsLocked(time.Now().Add(time.Duration(i) * time.Hour))
			}
			s.streams["work"] = true
			_ = s.watch(t.Context(), s.client, "work", "instance")
			if !s.state.Views["work"].ApprovalDirty {
				t.Fatal("permission event did not request authoritative refresh")
			}
			selected := s.takeObservationsLocked(time.Now())
			if !selected["worker:task"] {
				t.Fatal("new native approval did not renew this worker's observation")
			}
			if err := s.refreshSelectedWorkers(t.Context(), s.client, selected); err != nil {
				t.Fatal(err)
			}
			want := 1
			if settled {
				want = 0
			}
			if reads.Load() != 1 || len(s.Snapshot().Approvals) != want || s.state.Views["work"].ApprovalDirty {
				t.Fatalf("worker approval not reconciled: reads=%d approvals=%d dirty=%v", reads.Load(), len(s.Snapshot().Approvals), s.state.Views["work"].ApprovalDirty)
			}
		})
	}
}

func TestOldUnknownReceiptCannotStarveNewResidentWorker(t *testing.T) {
	var oldReads, workerReads, posts, streams atomic.Int32
	var started atomic.Bool
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/control/v1/initialize":
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: required})
		case "/api/control/v1/operations/old-unknown":
			oldReads.Add(1)
			http.NotFound(w, r)
		case "/api/control/v1/sessions/main/state":
			writeFixture(w, wire.SessionState{SessionId: "main"})
		case "/api/control/v1/sessions":
			posts.Add(1)
			var in wire.CreateSessionRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			writeFixture(w, wire.CommandResult{OperationId: value(in.OperationId), Outcome: "accepted", SessionId: pointer("fresh")})
		case "/api/control/v1/sessions/fresh/prompt":
			posts.Add(1)
			var in wire.PromptRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			started.Store(true)
			writeFixture(w, wire.CommandResult{OperationId: value(in.OperationId), Outcome: "accepted", Target: &wire.CommandTarget{TurnId: pointer("fresh-turn")}})
		case "/api/control/v1/sessions/fresh/state":
			workerReads.Add(1)
			writeFixture(w, wire.SessionState{SessionId: "fresh", Run: wire.RunState{TurnId: pointer("fresh-turn"), Active: pointer(started.Load()), Status: pointer("working")}})
		case "/api/control/v1/sessions/fresh/reconnect":
			streams.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	s.state.StoreID = "store"
	s.streams["main"] = true // Resident Runtime, not the retained-owner Connect loop.
	s.state.Operations["old-unknown"] = journal{Outcome: "unknown", Path: "/sessions/old/prompt", Digest: "original"}
	ctx, cancel := context.WithCancel(t.Context())
	s.ctx = ctx
	s.wg.Add(1)
	go s.pollLoop(ctx)
	t.Cleanup(func() { cancel(); s.wg.Wait() })
	deadline := time.Now().Add(12 * time.Second)
	for oldReads.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if oldReads.Load() != 3 {
		t.Fatal("original receipt did not exhaust its read budget", oldReads.Load())
	}
	out, err := s.StartWork(authorizedWorkerCall(t.Context(), s), api.WorkStart{ID: "fresh-task", Workspace: t.TempDir(), TaskStart: api.TaskStart{RequestID: "fresh-request", Title: "fixture", Prompt: "synthetic"}})
	if err != nil || out.Outcome != "accepted" {
		t.Fatal(out, err)
	}
	deadline = time.Now().Add(4 * time.Second)
	for streams.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	s.wg.Wait()
	if workerReads.Load() < 2 || streams.Load() != 1 || s.WorkStates()[0].Task.Status != "working" {
		t.Fatalf("new accepted worker was not observed: reads=%d streams=%d state=%s", workerReads.Load(), streams.Load(), s.WorkStates()[0].Task.Status)
	}
	if oldReads.Load() != 3 || posts.Load() != 2 || s.state.Operations["old-unknown"].Outcome != "unknown" || s.state.Workers["fresh-task"].Binding.SessionId != "fresh" {
		t.Fatal("new work retried an exhausted old receipt or replaced its owner", oldReads.Load(), posts.Load())
	}
}

func TestWorkerApprovalReadCannotReplaceNewerApproval(t *testing.T) {
	var s *Session
	s = fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
			t.Errorf("unexpected effect %s %s", r.Method, r.URL.Path)
			return
		}
		s.mu.Lock()
		applyEnvelope(s.state.Views["work"], wire.Envelope{Kind: "session/request_permission", EventId: pointer("new-event"), ApprovalRequestId: pointer("new-approval")})
		s.mu.Unlock()
		writeFixture(w, wire.SessionState{SessionId: "work"}) // Stale settled approval.
	})
	s.streams["work"] = true
	s.state.Workers["task"] = worker{Binding: wire.SessionBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "working"}}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Approval: wire.ApprovalState{Active: testApproval()}}, ApprovalDirty: true, Seen: map[string]bool{}}
	if err := s.refreshWorkers(t.Context(), s.client); err != nil {
		t.Fatal(err)
	}
	if v := s.state.Views["work"]; !v.ApprovalDirty || v.State.Approval.Active == nil {
		t.Fatal("stale Worker read overwrote a newer approval event")
	}
}

func TestUnknownOperationFailuresDoNotSkipOtherOriginalReceipts(t *testing.T) {
	var reads atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/api/control/v1/operations/original-") {
			t.Errorf("recovery dispatched %s %s", r.Method, r.URL.Path)
			return
		}
		reads.Add(1)
		w.WriteHeader(http.StatusForbidden)
		writeFixture(w, map[string]string{"code": "permission_denied"})
	})
	for _, id := range []string{"original-a", "original-b"} {
		s.state.Operations[id] = journal{Outcome: "unknown", Path: "/sessions/work/prompt", Digest: id}
	}
	if err := s.recoverOperations(t.Context()); err == nil || reads.Load() != 2 {
		t.Fatal("one failed original receipt starved another", reads.Load(), err)
	}
	for id, op := range s.state.Operations {
		if op.Outcome != "unknown" || op.Digest != id {
			t.Fatal("read rejection settled or replaced an unknown dispatch")
		}
	}
}

func TestExplicitObservationRecoveryRenewsSameCaelisOwner(t *testing.T) {
	var reads atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/operations/original" {
			t.Errorf("recovery dispatched %s %s", r.Method, r.URL.Path)
			return
		}
		reads.Add(1)
		http.NotFound(w, r)
	})
	s.streams["main"] = true
	s.state.Operations["original"] = journal{Outcome: "unknown", Path: "/sessions/work/prompt", Digest: "original-digest"}
	s.state.Workers["task"] = worker{Binding: wire.SessionBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "completed"}, Activity: "idle"}
	client, generation := s.client, s.generation
	for i := range 3 {
		selected := s.takeObservationsLocked(time.Now().Add(time.Duration(i) * time.Hour))
		if err := s.recoverSelectedOperations(t.Context(), selected); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(s.takeObservationsLocked(time.Now().Add(4*time.Hour))) != 0 {
		t.Fatal("ordinary Connect renewed exhausted receipt")
	}
	s.RetryObservations()
	if err := s.recoverSelectedOperations(t.Context(), s.takeObservationsLocked(time.Now())); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 4 || s.client != client || s.generation != generation || s.state.Workers["task"].Activity != "idle" || s.state.Operations["original"].Outcome != "unknown" {
		t.Fatal("explicit recovery lost owner/evidence or replayed an effect")
	}
}
