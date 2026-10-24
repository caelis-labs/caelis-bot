package machines

import (
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestAuditFirstUnknownPairCannotUseReplacementPeerAtSameAddress(t *testing.T) {
	old := newDualPeerTarget(t)
	replacement := newDualPeerTarget(t)
	var changed atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !changed.Load() && r.URL.Path == workerpeer.Prefix+"pair" {
			conn, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
				conn.Close()
			}
			return
		}
		if changed.Load() {
			replacement.peer.ServeHTTP(w, r)
		} else {
			old.peer.ServeHTTP(w, r)
		}
	}))
	defer proxy.Close()
	s, e := Open(t.TempDir(), &dualPeerLocal{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	oldCode, e := old.peer.Pairing(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	view, e := s.ConnectBot(t.Context(), api.BotConnectionInput{Address: proxy.URL, Name: "Same B address", Code: oldCode.Code})
	var unknown *api.BotConnectionUnconfirmedError
	if !errors.As(e, &unknown) {
		t.Fatal("expected original unknown", view, e)
	}
	if len(old.peer.Status().Sources) != 0 {
		t.Fatal("original B unexpectedly granted")
	}
	changed.Store(true)
	newCode, e := replacement.peer.Pairing(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	current, e := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: view.ID, Address: proxy.URL, Name: "Same B address", Code: newCode.Code})
	if e != nil || current.State != "ready" || len(replacement.peer.Status().Sources) != 1 {
		t.Fatal("explicit new authorization failed", current, e)
	}
	if _, e = s.ReadBotConnectionReceipt(t.Context(), unknown.Reference); e == nil {
		t.Fatal("new B grant at same URL certified original B which never received the pair")
	}
}
