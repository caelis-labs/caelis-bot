package weixin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestTypingFollowsOnlyPairedMainTurn(t *testing.T) {
	statuses := make(chan int, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/getconfig":
			_, _ = w.Write([]byte(`{"ret":0,"typing_ticket":"ticket"}`))
		case "/ilink/bot/sendtyping":
			var req struct {
				Status int `json:"status"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			statuses <- req.Status
		default:
			t.Errorf("unexpected typing path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	var current atomic.Value
	current.Store(api.Snapshot{Connection: "ready", Phase: "working", CurrentTurn: "turn", Items: []api.Item{{Kind: "user", TurnKey: "turn", RequestID: "local:1"}}})
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return current.Load().(api.Snapshot) }, Recovery: func() api.RecoveryState { return api.RecoveryState{} }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.Enabled, b.state.OwnerID = true, "owner"
	p := &protocol{client: server.Client(), base: server.URL, token: "secret"}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { b.typing(ctx, p); close(done) }()
	select {
	case status := <-statuses:
		t.Fatalf("local turn sent typing %d", status)
	case <-time.After(1100 * time.Millisecond):
	}
	current.Store(api.Snapshot{Connection: "ready", Phase: "working", CurrentTurn: "turn", Items: []api.Item{{Kind: "user", TurnKey: "turn", RequestID: "weixin:bot:1"}}})
	select {
	case status := <-statuses:
		if status != 1 {
			t.Fatalf("typing start: %d", status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("typing did not start")
	}
	current.Store(api.Snapshot{Connection: "ready", Phase: "idle", CurrentTurn: "turn"})
	select {
	case status := <-statuses:
		if status != 2 {
			t.Fatalf("typing cancel: %d", status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("typing was not cancelled")
	}
	cancel()
	<-done
}
