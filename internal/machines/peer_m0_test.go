package machines

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestPeerTemporaryNativeFailureReservesOriginalCapacity(t *testing.T) {
	target := newDualPeerTarget(t)
	s, m, _, machine, _ := dualPeerConnect(t, target)
	m.ConfigureLimit(func() int { return 1 })
	first, err := m.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "first-capacity", Title: "first", Prompt: "run"})
	if err != nil {
		t.Fatal(err)
	}
	owner := target.owner(s.state.ProfileID, "codex")
	owner.connectFailure.Store(true)
	for _, poll := range s.workPolls() {
		s.pollWork(t.Context(), poll)
	}
	if _, err = m.ReadTask(t.Context(), first.ID); err == nil {
		t.Fatal("temporary failure was hidden")
	}
	owner.connectFailure.Store(false)
	_, err = m.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "second-capacity", Title: "second", Prompt: "run"})
	if err == nil {
		t.Fatal("temporary native handshake failure released still-running capacity")
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.starts != 1 {
		t.Fatal("another worker was dispatched", owner.starts)
	}
}

func TestPeerFailedPreparationDoesNotStrandPairingProof(t *testing.T) {
	target := newDualPeerTarget(t)
	s, m, _, machine, _ := dualPeerConnect(t, target)
	_, err := m.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "bad-workspace", Title: "bad", Prompt: "run", Workspace: "relative/path"})
	if err == nil {
		t.Fatal("relative workspace accepted")
	}
	if len(s.state.Routes) != 0 {
		t.Fatal("A retained preparation")
	}
	if err = s.RemoveMachine(t.Context(), machine); err != nil {
		t.Fatal("undispatched preparation stranded source", err)
	}
	if len(target.peer.Status().Sources) != 0 {
		t.Fatal("A discarded token before B released undispatched preparation")
	}
	code, err := target.peer.Pairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConnectBot(t.Context(), api.BotConnectionInput{Name: "restored", Address: target.http.URL, Code: code.Code}); err != nil {
		t.Fatal("normal re-pair cannot recover", err)
	}
}

func TestPeerRemovalDoesNotDiscardUnconfirmedProof(t *testing.T) {
	target := newDualPeerTarget(t)
	s, _, _, machine, _ := dualPeerConnect(t, target)
	before := s.state.Profiles[machine].Peer
	target.offline.Store(true)
	if err := s.RemoveMachine(t.Context(), machine); err == nil {
		t.Fatal("unconfirmed unpair discarded original proof")
	}
	if s.state.Profiles[machine].Peer != before {
		t.Fatal("only original credential was changed")
	}
}

func TestPeerStreamsFollowRepairedAddressAndFenceOldErrors(t *testing.T) {
	target := newDualPeerTarget(t)
	s, m, _, machine, _ := dualPeerConnect(t, target)
	task, err := m.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "stream-original", Title: "stream", Prompt: "run"})
	if err != nil {
		t.Fatal(err)
	}
	var oldRequests, newRequests atomic.Int32
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { oldRequests.Add(1); <-r.Context().Done() }))
	defer old.Close()
	s.mu.Lock()
	p := s.state.Profiles[machine]
	p.Peer.Address = old.URL
	s.state.Profiles[machine] = p
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); s.peerStreams(ctx) }()
	defer func() { cancel(); <-done }()
	waitPeerTest(t, func() bool { return oldRequests.Load() > 0 })
	next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { newRequests.Add(1); target.peer.ServeHTTP(w, r) }))
	t.Cleanup(next.Close)
	code, err := target.peer.Pairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConnectBot(t.Context(), api.BotConnectionInput{ID: machine, Name: "same B", Address: next.URL, Code: code.Code}); err != nil {
		t.Fatal(err)
	}
	waitPeerTest(t, func() bool { return newRequests.Load() > 1 })
	waitPeerTest(t, func() bool {
		for _, state := range s.WorkStates() {
			if state.Task.ID == task.ID {
				return state.Task.Status == "working"
			}
		}
		return false
	})
	// Changing address preserves the paired identity and original native owner.
	if s.state.Profiles[machine].Peer.Peer != target.peer.Status().Peer {
		t.Fatal("address repair changed target identity")
	}
}

func waitPeerTest(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("peer behavior did not converge before bounded deadline")
}
