package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestCallbackRecoveryThenWaitKeepsClaimedReceipts(t *testing.T) {
	var snapshots, waits, receipts atomic.Int32
	entered, submitted := make(chan struct{}), make(chan struct{})
	old := wire.ApplicationCall{Id: "old", SessionId: "main", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", TurnId: "turn", ItemId: "item", State: "claimed", Source: wire.ApplicationSource{Kind: "user", OperationId: "request"}}
	next := old
	next.Id, next.State = "new", "pending"
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/calls") && r.URL.Query().Get("wait") == "true":
			if waits.Add(1) == 1 {
				close(entered)
				_ = json.NewEncoder(w).Encode([]wire.ApplicationCall{next})
			} else {
				if waits.Load() == 2 {
					close(submitted)
				}
				<-r.Context().Done()
			}
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/calls"):
			snapshots.Add(1)
			_ = json.NewEncoder(w).Encode([]wire.ApplicationCall{old})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/new/claim"):
			call := next
			call.State = "claimed"
			_ = json.NewEncoder(w).Encode(call)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/result"):
			receipts.Add(1)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Error("unexpected callback route")
		}
	})
	s.state.Operations["request"] = journal{Path: "/application/sessions/main/prompt", Source: old.Source, Outcome: "committed"}
	s.state.Calls[old.Id] = callRecord{Call: old, Phase: "receipt", Receipt: &wire.ApplicationCallResult{Outcome: "succeeded", Content: json.RawMessage(`[]`)}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.wg.Add(1)
	go s.callLoop(ctx)
	for _, ch := range []chan struct{}{entered, submitted} {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatal("callback recovery or wait did not complete")
		}
	}
	cancel()
	s.wg.Wait()
	if snapshots.Load() != 1 || receipts.Load() != 2 || s.state.Calls[old.Id].Phase != "completed" || s.state.Calls[next.Id].Phase != "completed" {
		t.Fatal("callback recovery omitted original receipt or repeated full history")
	}
}

func TestIdleCallbackWaitCancellationIsNotDisconnection(t *testing.T) {
	entered := make(chan struct{})
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := waitCalls(ctx, s.client, "main"); done <- err }()
	<-entered
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("idle cancellation presented a disconnect", err)
		}
	case <-time.After(time.Second):
		t.Fatal("callback wait ignored cancellation")
	}
}
