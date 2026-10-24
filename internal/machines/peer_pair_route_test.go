package machines

import (
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuditOriginalPairReceiptCannotRollBackNewReachableAddress(t *testing.T) {
	target := newDualPeerTarget(t)
	s, _, _, id, _ := dualPeerConnect(t, target)
	s.mu.Lock()
	original := s.state.Profiles[id]
	ref := s.botConnectionReference(original, original.Peer.Address)
	s.mu.Unlock()
	next := httptest.NewServer(target.peer)
	defer next.Close()
	updated, err := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: id, Name: "Renamed B", Address: next.URL})
	if err != nil || updated.Address != next.URL {
		t.Fatal("explicit address repair failed", updated, err)
	}
	if _, err = s.ReadBotConnectionReceipt(t.Context(), ref); err != nil {
		t.Fatal("same original peer/proof not readable", err)
	}
	s.mu.Lock()
	current := s.state.Profiles[id]
	s.mu.Unlock()
	if current.Peer.Address != next.URL || current.View.Address != next.URL {
		t.Fatalf("old receipt overwrote later explicit address: got %s view %s want %s", current.Peer.Address, current.View.Address, next.URL)
	}
}

func TestUnknownFirstPairCannotBeCertifiedByLaterDifferentPeer(t *testing.T) {
	first := newDualPeerTarget(t)
	drop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == workerpeer.Prefix+"pair" {
			c, _, _ := w.(http.Hijacker).Hijack()
			c.Close()
			return
		}
		first.peer.ServeHTTP(w, r)
	}))
	defer drop.Close()
	s, err := Open(t.TempDir(), &dualPeerLocal{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	code, err := first.peer.Pairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ConnectBot(t.Context(), api.BotConnectionInput{ID: "original-profile", Address: drop.URL, Code: code.Code, Name: "Original B"})
	var uncertain *api.BotConnectionUnconfirmedError
	if !errors.As(err, &uncertain) || uncertain.Reference.Peer != "" {
		t.Fatal("fixture did not retain original unknown identity", err)
	}
	ref := uncertain.Reference
	second := newDualPeerTarget(t)
	nextCode, err := second.peer.Pairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: ref.ID, Address: second.http.URL, Code: nextCode.Code, Name: "Explicit new B", Reconnect: true})
	if err != nil {
		t.Fatal("new explicit authorisation fixture", err)
	}
	if _, err = s.ReadBotConnectionReceipt(t.Context(), ref); err == nil {
		t.Fatal("different newly authorised peer certified old unknown grant")
	}
	s.mu.Lock()
	after := s.state.Profiles[ref.ID].View
	s.mu.Unlock()
	if after.Address != current.Address || after.PeerID != current.PeerID {
		t.Fatal("old unknown changed new connection", after, current)
	}
}
