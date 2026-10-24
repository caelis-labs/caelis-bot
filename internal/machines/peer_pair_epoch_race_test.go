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

func TestAuditPairEpochMustRemainOriginalAcrossReceiptNetworkRead(t *testing.T) {
	old := newDualPeerTarget(t)
	next := newDualPeerTarget(t)
	var changed, blockNext atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == workerpeer.Prefix+"call" && blockNext.CompareAndSwap(true, false) {
			close(entered)
			<-release
		}
		if !changed.Load() && r.URL.Path == workerpeer.Prefix+"pair" {
			conn, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
				conn.Close()
			}
			return
		}
		if changed.Load() {
			next.peer.ServeHTTP(w, r)
		} else {
			old.peer.ServeHTTP(w, r)
		}
	}))
	defer proxy.Close()
	s, e := Open(t.TempDir(), &dualPeerLocal{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	code, _ := old.peer.Pairing(t.Context())
	view, e := s.ConnectBot(t.Context(), api.BotConnectionInput{Address: proxy.URL, Name: "Original URL", Code: code.Code})
	var unknown *api.BotConnectionUnconfirmedError
	if !errors.As(e, &unknown) {
		t.Fatal(e)
	}
	blockNext.Store(true)
	done := make(chan error, 1)
	go func() { _, err := s.ReadBotConnectionReceipt(t.Context(), unknown.Reference); done <- err }()
	<-entered
	changed.Store(true)
	newCode, _ := next.peer.Pairing(t.Context())
	_, e = s.ConnectBot(t.Context(), api.BotConnectionInput{ID: view.ID, Address: proxy.URL, Name: "Original URL", Code: newCode.Code})
	if e != nil {
		close(release)
		t.Fatal("new explicit pair", e)
	}
	close(release)
	if e = <-done; e == nil {
		t.Fatal("changed pairing epoch during original receipt read certified replacement grant")
	}
}
