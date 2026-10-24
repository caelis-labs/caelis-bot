package machines

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

func TestUnknownBotProofRetainedBeforeWriteThroughArchiveAndReopen(t *testing.T) {
	target := newDualPeerTarget(t)
	s, e := Open(t.TempDir(), &dualPeerLocal{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	var writes atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == workerpeer.Prefix+"pair" {
			s.mu.Lock()
			var p profile
			for _, v := range s.state.Profiles {
				p = v
			}
			s.mu.Unlock()
			if !p.RetainPairingProof || p.PairingCodeDigest == "" || p.PairingEpoch == "" {
				t.Error("sole original proof not retained before native write")
			}
			if writes.Add(1) == 1 {
				c, _, _ := w.(http.Hijacker).Hijack()
				c.Close()
				return
			}
		}
		target.peer.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	code, e := target.peer.Pairing(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.ConnectBot(t.Context(), api.BotConnectionInput{Name: "B", Address: proxy.URL, Code: code.Code})
	var unknown *api.BotConnectionUnconfirmedError
	if !errors.As(e, &unknown) {
		t.Fatal(e)
	}
	id := unknown.Reference.ID
	original := s.state.Profiles[id]
	source := s.state.ProfileID
	if e = s.RemoveMachine(t.Context(), id); e != nil {
		t.Fatal("unknown without grant cannot be archived", e)
	}
	if !s.state.Profiles[id].View.Archived {
		t.Fatal("unknown proof deleted")
	}
	s, e = Open(s.root, &dualPeerLocal{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	ref, e := s.BotAuthorizationReference(id)
	if e != nil {
		t.Fatal(e)
	}
	fresh, e := target.peer.Pairing(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	got, _, e := s.AuthorizeBotConnection(t.Context(), ref, api.BotConnectionInput{ID: id, Address: proxy.URL, Code: fresh.Code, Name: "B"})
	if e != nil || got.State != "ready" || got.Archived {
		t.Fatal(got, e)
	}
	if s.state.ProfileID != source || s.state.Profiles[id].Peer.Token != original.Peer.Token || writes.Load() != 2 || len(target.peer.Status().Sources) != 1 {
		t.Fatal("fresh authorization replaced proof/source or replayed original")
	}
	if _, e = s.ReadBotConnectionReceipt(t.Context(), unknown.Reference); e == nil {
		t.Fatal("new authorization certified old unknown")
	}
}
