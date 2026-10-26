package caelis

import (
	"encoding/json"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWorkerFailureDoesNotClassifyOrExposeNativeProse(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected request") })
	head := nativeWorkHead("failed-turn")
	head.Run.Active = pointer(false)
	head.Run.Status = pointer("failed")
	addServiceWorker(s, "task", "work", head)
	s.state.Views["work"].Failure = "Selected model is at capacity. secret-token"
	f := s.workerViewLocked(s.state.Workers["task"]).Failure
	b, _ := json.Marshal(f)
	if f == nil || f.Code != "unknown" || f.Source != "caelis" || f.Retryable != nil || strings.Contains(string(b), "secret-token") || strings.Contains(string(b), "capacity") {
		t.Fatal("untyped cause gained authority", string(b))
	}
	saved := clone(s.state)
	s.state = saved
	if got := s.workerViewLocked(s.state.Workers["task"]).Failure; got.Execution != f.Execution {
		t.Fatal("restart changed failure association")
	}
	s.state.Views["work"].State = nativeWorkHead("next")
	if s.workerViewLocked(s.state.Workers["task"]).Failure != nil {
		t.Fatal("new turn inherited old failure")
	}
}

func TestWorkerStopAcknowledgementUsesFullOriginalTarget(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected request") })
	head := nativeWorkHead("one")
	addServiceWorker(s, "task", "work", head)
	key := workStopExecutionKey(head)
	stop := api.WorkStop{ID: "task", RequestID: "stop-original", ExecutionKey: key}
	s.state.Operations["stop"] = journal{Work: &workIntent{TaskID: "task", RequestID: stop.RequestID, Kind: "stop", Fingerprint: api.WorkFingerprint(stop)}, Outcome: "accepted"}
	if !s.WorkStates()[0].StopAcknowledged {
		t.Fatal("acknowledged stop not projected")
	}
	s.state = clone(s.state)
	if !s.WorkStates()[0].StopAcknowledged {
		t.Fatal("stop receipt lost after round trip")
	}
	head.Run.HandleId = pointer("replacement")
	s.state.Views["work"].State = head
	if s.WorkStates()[0].StopAcknowledged {
		t.Fatal("same turn replacement inherited stop")
	}
	s.state.Views["work"].State = nativeWorkHead("one")
	for _, outcome := range []string{"unknown", "rejected", "conflicted"} {
		receipt := s.state.Operations["stop"]
		receipt.Outcome = outcome
		s.state.Operations["stop"] = receipt
		if s.WorkStates()[0].StopAcknowledged {
			t.Fatal("unconfirmed stop suppressed report", outcome)
		}
	}
}

func TestWorkerStopCanonicalTargetSurvivesIdleBootstrap(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected request") })
	head := nativeWorkHead("one")
	addServiceWorker(s, "task", "work", head)
	target := wire.TurnTarget{HandleId: "h", RunId: "r-one", TurnId: "one"}
	stop := api.WorkStop{ID: "task", RequestID: "original-stop", ExecutionKey: workStopExecutionKey(head)}
	s.state.Operations["stop"] = journal{StopTarget: &target, Work: &workIntent{TaskID: "task", RequestID: stop.RequestID, Kind: "stop", Fingerprint: api.WorkFingerprint(stop)}, Outcome: "accepted"}
	p := s.state.Views["work"]
	s.applyScheduledEnvelope(p, wire.Envelope{Kind: "caelis/lifecycle", SessionId: pointer("work"), TurnId: pointer("one"), RunId: pointer("r-one"), HandleId: pointer("h"), Lifecycle: &wire.LifecycleEvent{State: "cancelled"}}, inputScope{SessionID: "work"})
	// Real idle Core shape: active handle absent; runId belongs to the SDK.
	p.State.Run = wire.RunState{Active: pointer(false), Status: pointer("interrupted"), RunId: pointer("sdk-run")}
	s.state = clone(s.state)
	states := s.WorkStates()
	if !states[0].StopAcknowledged || states[0].ExecutionKey != "one" {
		t.Fatal("canonical target lost after idle/restart", states)
	}
	p = s.state.Views["work"]
	s.applyScheduledEnvelope(p, wire.Envelope{Kind: "caelis/lifecycle", SessionId: pointer("work"), TurnId: pointer("two"), RunId: pointer("r-two"), HandleId: pointer("h-two"), Lifecycle: &wire.LifecycleEvent{State: "running"}}, inputScope{SessionID: "work"})
	s.applyScheduledEnvelope(p, wire.Envelope{Kind: "caelis/lifecycle", SessionId: pointer("work"), TurnId: pointer("two"), RunId: pointer("r-two"), HandleId: pointer("h-two"), Lifecycle: &wire.LifecycleEvent{State: "cancelled"}}, inputScope{SessionID: "work"})
	p.State.Run = wire.RunState{Active: pointer(false), Status: pointer("interrupted"), RunId: pointer("sdk-next")}
	if s.WorkStates()[0].StopAcknowledged || s.WorkStates()[0].ExecutionKey != "two" {
		t.Fatal("message-free later turn inherited original stop")
	}
}

func TestWorkerStopTargetSurvivesCanonicalReplacement(t *testing.T) {
	for _, kind := range []string{"same", "different_handle", "different_run", "later_turn", "empty_history"} {
		t.Run(kind, func(t *testing.T) {
			bootstrap := wire.SessionState{SessionId: "work", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", Run: wire.RunState{Active: pointer(false), Status: pointer("interrupted"), RunId: pointer("sdk-run")}}
			terminal := wire.Envelope{Kind: "caelis/lifecycle", SessionId: pointer("work"), TurnId: pointer("one"), Lifecycle: &wire.LifecycleEvent{State: "cancelled"}}
			switch kind {
			case "different_handle":
				terminal.HandleId = pointer("other")
			case "different_run":
				terminal.RunId = pointer("other")
			case "later_turn":
				terminal.TurnId = pointer("two")
			}
			events := []wire.Envelope{terminal}
			if kind == "empty_history" {
				events = nil
			}
			s := fixtureSession(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				b, _ := json.Marshal(bootstrap)
				fmt.Fprintf(w, "event: caelis.control.bootstrap\ndata: %s\n\n", b)
				for _, d := range []wire.SessionFeedDelivery{{Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("history")}, {Kind: "replace_page", Source: "replacement", SnapshotId: pointer("history"), Events: events}, {Kind: "replace_end", Source: "replacement", SnapshotId: pointer("history"), Page: pointer(1)}, {Kind: "sync", Source: "exact", NextCursor: pointer("complete")}} {
					b, _ := json.Marshal(d)
					fmt.Fprintf(w, "event: caelis.control.delivery\ndata: %s\n\n", b)
				}
			})
			head := nativeWorkHead("one")
			addServiceWorker(s, "task", "work", head)
			target := wire.TurnTarget{HandleId: "h", RunId: "r-one", TurnId: "one"}
			stop := api.WorkStop{ID: "task", RequestID: "original-stop", ExecutionKey: workStopExecutionKey(head)}
			s.state.Operations["stop"] = journal{StopTarget: &target, Work: &workIntent{TaskID: "task", RequestID: stop.RequestID, Kind: "stop", Fingerprint: api.WorkFingerprint(stop)}, Outcome: "accepted"}
			s.state.Views["work"].HeadTarget = target
			if err := s.watch(t.Context(), s.client, "work", "instance"); err != io.EOF {
				t.Fatal(err)
			}
			restored, err := loadBinding(s.path)
			if err != nil {
				t.Fatal(err)
			}
			s.state = restored
			if ack := s.WorkStates()[0].StopAcknowledged; ack != (kind == "same") {
				t.Fatalf("replacement %s stop confirmation=%t", kind, ack)
			}
			if kind == "later_turn" && s.WorkStates()[0].ExecutionKey != "two" {
				t.Fatal("message-free successor was lost")
			}
		})
	}
}

func TestWorkerStopAckSaveFailureRemainsUnconfirmed(t *testing.T) {
	head := nativeWorkHead("one")
	blocked := t.TempDir()
	var s *Session
	s = fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/state"):
			writeFixture(w, head)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/cancel"):
			var req wire.CancelRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			s.mu.Lock()
			s.path = blocked
			s.mu.Unlock()
			writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "accepted"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	addServiceWorker(s, "task", "work", head)
	originalPath := s.path
	if _, err := s.StopWorkRequest(t.Context(), api.WorkStop{ID: "task", RequestID: "stop-original", ExecutionKey: workStopExecutionKey(head)}); err == nil {
		t.Fatal("save failure hidden")
	}
	if s.WorkStates()[0].StopAcknowledged {
		t.Fatal("unpersisted ACK suppresses interruption")
	}
	restored, err := loadBinding(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	s.state = restored
	s.path = originalPath
	if s.WorkStates()[0].StopAcknowledged {
		t.Fatal("restart invented confirmed stop")
	}
}

func TestWorkerStateReadsRetainMessageFreeSuccessor(t *testing.T) {
	for _, saveFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(saveFailure), func(t *testing.T) {
			var reads atomic.Int32
			next := nativeWorkHead("two")
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/state") {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				state := next
				if reads.Add(1) > 1 {
					state.Run = wire.RunState{Active: pointer(false), Status: pointer("interrupted"), RunId: pointer("sdk-two")}
				}
				writeFixture(w, state)
			})
			head := nativeWorkHead("one")
			addServiceWorker(s, "task", "work", head)
			target := wire.TurnTarget{HandleId: "h", RunId: "r-one", TurnId: "one"}
			s.state.Views["work"].HeadTarget = target
			stop := api.WorkStop{ID: "task", RequestID: "original-stop", ExecutionKey: workStopExecutionKey(head)}
			s.state.Operations["stop"] = journal{StopTarget: &target, Work: &workIntent{TaskID: "task", RequestID: stop.RequestID, Kind: "stop", Fingerprint: api.WorkFingerprint(stop)}, Outcome: "accepted"}
			if saveFailure {
				s.path = t.TempDir()
			}
			_, err := s.ReadWork(t.Context(), "task")
			if saveFailure {
				if err == nil || s.state.Views["work"].HeadTarget != target {
					t.Fatal("failed read did not roll back target", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.ReadWork(t.Context(), "task"); err != nil {
				t.Fatal(err)
			}
			restored, err := loadBinding(s.path)
			if err != nil {
				t.Fatal(err)
			}
			s.state = restored
			state := s.WorkStates()[0]
			if state.ExecutionKey != "two" || state.StopAcknowledged {
				t.Fatal("idle read reused old turn's stop", state)
			}
		})
	}
}
