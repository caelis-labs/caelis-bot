package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

type boundPeerOwner struct {
	*workerFixture
	decisions int
}

func (w *boundPeerOwner) WorkCapabilities(context.Context, string) (api.WorkCapabilities, error) {
	return api.WorkCapabilities{Contract: api.WorkerContract, Owner: "original-foreign-owner", Build: "fixture", Runtime: "codex", History: true, Observe: true, Interactions: true, Files: true, Reconcile: true}, nil
}
func (w *boundPeerOwner) ObserveWork(ctx context.Context, id string) (api.WorkObservation, error) {
	return api.WorkObservation{State: w.states[id], Approvals: []api.Approval{{ID: "original-request", TaskID: id, Status: "pending"}}}, nil
}
func (w *boundPeerOwner) DecideWork(context.Context, api.WorkDecision) error {
	w.decisions++
	return nil
}
func (w *boundPeerOwner) ReadWorkConversation(context.Context, string) ([]api.TaskConversationMessage, error) {
	return []api.TaskConversationMessage{}, nil
}

func TestBoundPeerReadObserveDecisionIgnoreResidentConfiguration(t *testing.T) {
	resident := &disconnectedConversationEngine{testEngine: newTestEngine()}
	root := t.TempDir()
	if e := localstate.Write(filepath.Join(root, "runtime.json"), api.RuntimeSettings{Runtime: "codex", CLIPath: "/unused-native"}); e != nil {
		t.Fatal(e)
	}
	a, e := newApplication(root, Host{}, func(id string) (providerFactory, error) {
		return providerFactory{ID: id, Open: func(providerConfig) (api.Engine, error) { return resident, nil }}, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = a.Close() })
	owner := &boundPeerOwner{workerFixture: &workerFixture{runtime: "codex", states: map[string]api.WorkState{"original-task": {Task: api.Task{ID: "original-task", Status: "working"}, Activity: "active"}}}}
	a.peer.owners["original-source\x00codex"] = owner
	peerRoot := filepath.Join(root, "bound-peer")
	data := map[string]any{"version": 1, "identity": "B", "enabled": true, "grants": map[string]any{"original-source": map[string]any{"peer": "original-source", "hash": fmt.Sprintf("%x", sha256.Sum256([]byte("proof")))}}, "bindings": map[string]any{"original-source": map[string]string{"original-task": "codex"}}}
	if e = localstate.Write(filepath.Join(peerRoot, "peers.json"), data); e != nil {
		t.Fatal(e)
	}
	server, e := workerpeer.Open(peerRoot, a.openForeignWorker)
	if e != nil {
		t.Fatal(e)
	}
	h := httptest.NewServer(server)
	defer h.Close()
	conn := workerpeer.Connection{Address: h.URL, Peer: "B", Token: "proof"}
	for _, req := range []workerpeer.Request{{Action: "read", ID: "original-task"}, {Action: "observe", ID: "original-task"}, {Action: "messages", ID: "original-task"}, {Action: "decision", Decision: api.WorkDecision{TaskID: "original-task", RequestID: "decision-op", Decision: api.Decision{ID: "original-request", Choice: "approve"}}}} {
		if _, e = workerpeer.Call(t.Context(), conn, req); e != nil {
			t.Errorf("bound %s depended on offline resident: %v", req.Action, e)
		}
	}
	if resident.modelReads != 0 || owner.decisions != 1 {
		t.Fatal("resident consulted or native decision lost", resident.modelReads, owner.decisions)
	}
	if _, _, _, e = a.openForeignWorker(t.Context(), "new-source", "", filepath.Join(root, "new-source")); e == nil {
		t.Fatal("new admission skipped current resident")
	}
}

func (w *boundPeerOwner) WorkApprovalURL(context.Context, string, string) (string, error) {
	return "https://example.invalid/original", nil
}
