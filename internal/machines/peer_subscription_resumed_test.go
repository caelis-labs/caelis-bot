package machines

import (
	"context"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

func TestPeerReturnedSubscriptionReapedWhenTaskResumesSameConnection(t *testing.T) {
	connection := workerpeer.Connection{Address: "http://127.0.0.1:1", Peer: "original-peer", Token: "test-proof"}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	running := map[string]peerSubscription{
		"original-task": {cancel: cancel, done: done, connection: connection, runtime: "codex"},
	}
	// Terminal state retires its stream, but the Watch is still unwinding.
	reapPeerStreams(running, map[string]profile{}, map[string]string{})
	if ctx.Err() == nil || len(running) != 1 {
		t.Fatal("completed task did not retain canceled Watch until return")
	}
	// User continues the original task before the next scheduling round. The
	// connection/runtime match, but cancellation cannot revive this old Watch.
	close(done)
	targets := map[string]profile{"original-task": {Peer: connection}}
	runtimes := map[string]string{"original-task": "codex"}
	reapPeerStreams(running, targets, runtimes)
	if _, exists := running["original-task"]; exists {
		t.Fatal("returned canceled Watch permanently occupies resumed task's subscription")
	}
}
