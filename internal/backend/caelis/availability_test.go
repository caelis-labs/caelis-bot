package caelis

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"net/http"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestUnknownWorkerOperationDoesNotDisableMainInput(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	s.state.Workers["original-worker"] = worker{Binding: wire.SessionBinding{SessionId: "worker-session"}}
	s.state.Operations["original-worker-request"] = journal{Outcome: "unknown", Path: "/sessions/worker-session/prompt"}
	if !s.Snapshot().CanSend {
		t.Fatal("local worker uncertainty disabled healthy main input")
	}
	s.state.Operations["original-main-request"] = journal{Outcome: "unknown", Path: "/sessions/main/prompt"}
	if s.Snapshot().CanSend {
		t.Fatal("main native dispatch uncertainty lost original fence")
	}
}

func TestScheduledBusyAdmissionIsExplicitlyDeferred(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("busy request reached native server")
		http.NotFound(w, r)
	})
	s.state.Operations["busy-user-request"] = journal{Outcome: "unknown", Path: "/sessions/main/prompt"}
	receipt, err := s.Submit(t.Context(), api.Submission{ID: "event-wake-busy", Text: "Continue", Scheduled: true}, nil)
	if err != nil || receipt.Outcome != "rejected" || !receipt.Deferred {
		t.Fatal(receipt, err)
	}
	if _, ok := s.state.Operations["event-wake-busy"]; ok {
		t.Fatal("deferred request created operation")
	}
}
