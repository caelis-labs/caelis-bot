package caelis

import (
	"net/http"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestUnknownWorkerOperationDoesNotDisableMainInput(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	s.state.Workers["original-worker"] = worker{Binding: wire.ApplicationBinding{SessionId: "worker-session"}}
	s.state.Operations["original-worker-request"] = journal{Outcome: "unknown", Path: "/application/sessions/worker-session/prompt"}
	if !s.Snapshot().CanSend {
		t.Fatal("local worker uncertainty disabled healthy main input")
	}
	s.state.Operations["original-main-request"] = journal{Outcome: "unknown", Path: "/application/sessions/main/prompt"}
	if s.Snapshot().CanSend {
		t.Fatal("main native dispatch uncertainty lost original fence")
	}
}
