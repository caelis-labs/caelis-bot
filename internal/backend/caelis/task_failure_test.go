package caelis

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"net/http"
	"strings"
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
