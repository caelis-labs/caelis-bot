package caelis

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestExactInterruptRejectsChangedAuthoritativeTargetBeforeRevocation(t *testing.T) {
	var cancels, revocations atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/state") {
			writeFixture(w, wire.SessionState{Run: wire.RunState{Active: pointer(true), HandleId: pointer("handle"), RunId: pointer("new-run"), TurnId: pointer("new-turn")}})
			return
		}
		cancels.Add(1)
		t.Error("stale exact target reached cancel")
	})
	s.state.Views["main"].State.Run = wire.RunState{Active: pointer(true), HandleId: pointer("handle"), RunId: pointer("old-run"), TurnId: pointer("old-turn")}
	if err := s.InterruptTurn(t.Context(), "old-turn", func() { revocations.Add(1) }); err == nil {
		t.Fatal("new authoritative turn accepted")
	}
	if cancels.Load() != 0 || revocations.Load() != 0 {
		t.Fatal("stale turn revoked newer desktop authority")
	}
}

func TestExactInterruptCarriesFullNativeTargetAndRevokesBeforeCancel(t *testing.T) {
	var revoked atomic.Bool
	target := wire.TurnTarget{HandleId: "handle", RunId: "run", TurnId: "turn"}
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			writeFixture(w, wire.SessionState{Run: wire.RunState{Active: pointer(true), HandleId: pointer(target.HandleId), RunId: pointer(target.RunId), TurnId: pointer(target.TurnId)}})
			return
		}
		var in wire.CancelRequest
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Target != target || !revoked.Load() {
			t.Error("cancel lost target or preceding authority revocation")
		}
		writeFixture(w, wire.CommandResult{OperationId: value(in.OperationId), Outcome: "accepted"})
	})
	s.state.Views["main"].State.Run = wire.RunState{Active: pointer(true), HandleId: pointer(target.HandleId), RunId: pointer(target.RunId), TurnId: pointer(target.TurnId)}
	if err := s.InterruptTurn(t.Context(), target.TurnId, func() { revoked.Store(true) }); err != nil {
		t.Fatal(err)
	}
}
