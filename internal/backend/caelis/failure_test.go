package caelis

import (
	"net/http"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestNativeFailureSurvivesReconnectAndClearsOnNextTurn(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected request") })
	v := s.state.Views["main"]
	applyEnvelope(v, wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("first"), Lifecycle: &wire.LifecycleEvent{State: "running"}})
	applyEnvelope(v, wire.Envelope{Kind: "caelis/error", TurnId: pointer("first"), Error: pointer("model request failed after 5 retries")})
	applyEnvelope(v, wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("first"), Lifecycle: &wire.LifecycleEvent{State: "failed"}})
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	restored, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.state = restored
	snap := s.Snapshot()
	if snap.Phase != "failed" || !snap.CanSend || !strings.Contains(snap.Message, "model request failed after 5 retries") {
		t.Fatalf("native failure disappeared: %+v", snap)
	}
	v = s.state.Views["main"]
	applyEnvelope(v, wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("second"), Lifecycle: &wire.LifecycleEvent{State: "running"}})
	applyEnvelope(v, wire.Envelope{Kind: "caelis/error", Scope: pointer("participant"), Error: pointer("unrelated worker error")})
	applyEnvelope(v, wire.Envelope{Kind: "caelis/error", ApprovalRequestId: pointer("review"), Error: pointer("unrelated review error")})
	applyEnvelope(v, wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("second"), Lifecycle: &wire.LifecycleEvent{State: "completed"}})
	if snap := s.Snapshot(); snap.Phase != "completed" || snap.Message != "" || v.Failure != "" {
		t.Fatalf("previous or unrelated failure leaked into new turn: %+v", snap)
	}
}

func TestFailedHeadWithoutErrorStillExplainsNoReply(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected request") })
	v := s.state.Views["main"]
	v.State.Run.Status = pointer("failed")
	if snap := s.Snapshot(); snap.Message == "" || !snap.CanSend {
		t.Fatal("failed head became a silent empty response")
	}
	applyEnvelope(v, wire.Envelope{Kind: "caelis/lifecycle", Lifecycle: &wire.LifecycleEvent{State: "failed", Reason: pointer("provider unavailable")}})
	if !strings.Contains(s.Snapshot().Message, "provider unavailable") {
		t.Fatal("native lifecycle reason was discarded")
	}
}
