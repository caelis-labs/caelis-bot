package machines

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

func TestBotDefiniteFirstPairingRefusalCanBeRemovedAndCorrected(t *testing.T) {
	target := newDualPeerTarget(t)
	s, err := Open(t.TempDir(), &dualPeerLocal{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	code, err := target.peer.Pairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ConnectBot(t.Context(), api.BotConnectionInput{Name: "B", Address: target.http.URL, Code: "mistyped-code"})
	if err == nil {
		t.Fatal("wrong code accepted")
	}
	if len(target.peer.Status().Sources) != 0 || len(s.state.Routes) != 0 {
		t.Fatal("rejected pairing bound work")
	}
	for id := range s.state.Profiles {
		if err := s.RemoveMachine(t.Context(), id); err != nil {
			t.Fatal("definitely rejected first profile is not removable", err)
		}
	}
	if len(s.state.Profiles) != 0 {
		t.Fatal("failed profile remains")
	}
	s, err = Open(s.root, &dualPeerLocal{}, nil)
	if err != nil || len(s.state.Profiles) != 0 || len(s.state.Routes) != 0 {
		t.Fatal("definite refusal cleanup was not durable", err)
	}
	connected, err := s.ConnectBot(t.Context(), api.BotConnectionInput{Name: "B", Address: target.http.URL, Code: code.Code})
	if err != nil || connected.State != "ready" || len(target.peer.Status().Sources) != 1 {
		t.Fatal("corrected pairing failed", connected.State, err)
	}
}

func TestBotUnknownFirstPairingRetainsAndReconcilesOriginalProof(t *testing.T) {
	target := newDualPeerTarget(t)
	var drop atomic.Bool
	drop.Store(true)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == workerpeer.Prefix+"pair" && drop.CompareAndSwap(true, false) {
			accepted := httptest.NewRecorder()
			target.peer.ServeHTTP(accepted, r)
			if accepted.Code != http.StatusOK {
				t.Error("native pairing not accepted before response loss", accepted.Code)
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		target.peer.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	s, err := Open(t.TempDir(), &dualPeerLocal{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	code, err := target.peer.Pairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ConnectBot(t.Context(), api.BotConnectionInput{Name: "B", Address: proxy.URL, Code: code.Code})
	var rejected *api.ActionRejectedError
	if err == nil || errors.As(err, &rejected) || len(s.state.Profiles) != 1 || len(target.peer.Status().Sources) != 1 {
		t.Fatal("unknown pairing discarded or rejected", err)
	}
	var original profile
	for _, p := range s.state.Profiles {
		original = p
	}
	if original.Peer.Token == "" {
		t.Fatal("original proof missing")
	}
	s, err = Open(s.root, &dualPeerLocal{}, nil)
	if err != nil || s.state.Profiles[original.View.ID].Peer.Token != original.Peer.Token || len(s.state.Profiles) != 1 {
		t.Fatal("unknown original pairing proof did not survive reopen", err)
	}
	connected, err := s.ConnectBot(context.Background(), api.BotConnectionInput{ID: original.View.ID, Name: "B", Address: proxy.URL, Reconnect: true})
	if err != nil || connected.State != "ready" || s.state.Profiles[connected.ID].Peer.Token != original.Peer.Token || len(target.peer.Status().Sources) != 1 {
		t.Fatal("unknown pairing not reconciled with original proof", err)
	}
}

func TestBotInvalidAddressIsRejectedBeforeCreatingPairingProfile(t *testing.T) {
	s, err := Open(t.TempDir(), &dualPeerLocal{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ConnectBot(t.Context(), api.BotConnectionInput{Address: "http://127.0.0.1:9317/unsupported-path", Code: "fixture-code"})
	var rejected *api.ActionRejectedError
	if !errors.As(err, &rejected) || rejected.Code != "invalid_bot_address" || len(s.state.Profiles) != 0 {
		t.Fatal("correctable address refusal became an uncertain profile", err)
	}
}

func TestAbortingUndispatchedPairPreservesNewIntentAndOldUnknown(t *testing.T) {
	s, e := Open(t.TempDir(), &dualPeerLocal{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	in := api.BotConnectionInput{ID: "original-profile", Address: "http://127.0.0.1:1", Code: "old-uncertain-code"}
	old, e := s.PrepareBotConnection(in, nil)
	if e != nil {
		t.Fatal(e)
	}
	// Simulate the original attempt having crossed ingress admission. Its proof
	// must survive rollback of a new, explicitly distinct undispatched intent.
	in.Code = "new-unused-code"
	next, e := s.PrepareBotConnection(in, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.AbortPreparedBotConnection(next); e != nil {
		t.Fatal(e)
	}
	restored, e := s.BotAuthorizationReference(in.ID)
	if e != nil || restored.Pairing != old.Pairing || restored.Proof != old.Proof {
		t.Fatal("old unknown proof changed", e)
	}
	newer, e := s.PrepareBotConnection(in, nil)
	if e != nil {
		t.Fatal("unused new code was burned", e)
	}
	if e = s.AbortPreparedBotConnection(old); e != nil {
		t.Fatal(e)
	}
	current, e := s.BotAuthorizationReference(in.ID)
	if e != nil || current.Pairing != newer.Pairing {
		t.Fatal("late rollback erased newer preparation", e)
	}
}
