package codex

import (
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestExactInterruptRejectsStaleTurnWithoutRevokingAuthority(t *testing.T) {
	s, _ := sessionPair(t, "normal")
	if r := sendSynthetic(t, s, "exact-stale-submit"); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	awaitState(t, s, func(v api.Snapshot) bool { return v.CurrentTurn != "" })
	var before atomic.Int32
	if err := s.InterruptTurn(testContext(t), opaque("old-run"), func() { before.Add(1) }); err == nil {
		t.Fatal("stale turn accepted")
	}
	if before.Load() != 0 || s.Snapshot().CurrentTurn != opaque("run-native") {
		t.Fatal("stale request revoked/retargeted current turn")
	}
}

func TestExactInterruptDispatchesCapturedTargetAndWaitsForTerminal(t *testing.T) {
	s, f := sessionPair(t, "normal")
	if r := sendSynthetic(t, s, "exact-submit"); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	view := awaitState(t, s, func(v api.Snapshot) bool { return v.CurrentTurn != "" })
	var before atomic.Bool
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method != "turn/interrupt" {
			return nil, false
		}
		var in struct {
			Thread string `json:"threadId"`
			Turn   string `json:"turnId"`
		}
		_ = json.Unmarshal(m.Params, &in)
		if in.Thread != "thread-native" || in.Turn != "run-native" || !before.Load() {
			t.Error("native cancel changed captured target or observer order")
		}
		return nil, false // Continue fixture's authoritative terminal event.
	}
	f.mu.Unlock()
	if err := s.InterruptTurn(testContext(t), view.CurrentTurn, func() { before.Store(true) }); err != nil {
		t.Fatal(err)
	}
	if !before.Load() || s.Snapshot().CurrentTurn != "" || s.Snapshot().Phase != "interrupted" {
		t.Fatal("exact interrupt returned before terminal projection")
	}
}
