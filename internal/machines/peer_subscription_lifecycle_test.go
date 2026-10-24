package machines

import (
	"context"
	"fmt"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

// No clock or HTTP scheduling participates: a canceled Watch may remain in
// its deferred response-body/native callback cleanup until done is closed.
func TestPeerCanceledSubscriptionsHoldBudgetUntilWatchReturns(t *testing.T) {
	running := map[string]peerSubscription{}
	done := make([]chan struct{}, 16)
	cancelled := make([]bool, 16)
	for i := range 16 {
		done[i] = make(chan struct{})
		running[fmt.Sprint(i)] = peerSubscription{
			cancel: func() { cancelled[i] = true },
			done:   done[i],
		}
	}
	// All old tasks became historical. Cancellation is requested immediately,
	// but none of their Watch calls has yet unwound.
	reapPeerStreams(running, map[string]profile{}, map[string]string{})
	for i, cancelled := range cancelled {
		if !cancelled {
			t.Fatalf("historical stream %d was not canceled", i)
		}
	}
	if len(running) != 16 {
		t.Fatalf("canceled Watch prematurely released budget: got %d want 16", len(running))
	}
	// A repeat observation round cannot release a still-unwinding Watch.
	reapPeerStreams(running, map[string]profile{}, map[string]string{})
	if len(running) != 16 {
		t.Fatalf("later round released live Watch: got %d want 16", len(running))
	}
	// Exactly one Watch returns; exactly one budget slot becomes reusable.
	close(done[0])
	reapPeerStreams(running, map[string]profile{}, map[string]string{})
	if len(running) != 15 {
		t.Fatalf("returned Watch was not reaped: got %d want 15", len(running))
	}
	if _, exists := running["0"]; exists {
		t.Fatal("returned Watch retained its task slot")
	}
	for i := 1; i < 16; i++ {
		close(done[i])
	}
	reapPeerStreams(running, map[string]profile{}, map[string]string{})
	if len(running) != 0 {
		t.Fatal("returned historical Watches retained budget", len(running))
	}
}

func TestPeerChangedConnectionCannotReplaceUnwindingWatch(t *testing.T) {
	old := workerpeer.Connection{Address: "http://127.0.0.1:1", Peer: "original-peer", Token: "test-proof"}
	next := old
	next.Address = "http://127.0.0.1:2"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	running := map[string]peerSubscription{
		"original-task": {cancel: cancel, done: done, connection: old, runtime: "codex"},
	}
	targets := map[string]profile{"original-task": {Peer: next}}
	runtimes := map[string]string{"original-task": "codex"}
	reapPeerStreams(running, targets, runtimes)
	if ctx.Err() == nil {
		t.Fatal("changed connection did not cancel old Watch")
	}
	if len(running) != 1 || running["original-task"].connection != old {
		t.Fatal("new connection replaced old generation before Watch returned")
	}
	close(done)
	reapPeerStreams(running, targets, runtimes)
	if len(running) != 0 {
		t.Fatal("old generation was not released after Watch returned")
	}
}
