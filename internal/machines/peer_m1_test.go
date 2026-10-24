package machines

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

func TestBotRenameIsOfflineAndAddressRepairRetainsOriginalTask(t *testing.T) {
	target := newDualPeerTarget(t)
	s, m, local, machine, _ := dualPeerConnect(t, target)
	task, err := m.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "m1-address-task", Title: "Original", Prompt: "run"})
	if err != nil {
		t.Fatal(err)
	}
	before := s.state.Profiles[machine]
	target.offline.Store(true)
	renamed, err := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: machine, Address: target.http.URL, Name: "Renamed while offline"})
	if err != nil || renamed.Name != "Renamed while offline" || renamed.CheckedAt != before.View.CheckedAt || s.state.Profiles[machine].Peer != before.Peer {
		t.Fatal("rename required network or changed proof", renamed, err)
	}
	next := httptest.NewServer(target.peer)
	defer next.Close()
	repaired, err := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: machine, Address: next.URL, Name: renamed.Name})
	if err != nil || repaired.Address != next.URL || repaired.PeerID != before.View.PeerID || s.state.Profiles[machine].Peer.Token != before.Peer.Token {
		t.Fatal("address repair required pairing or changed identity", repaired, err)
	}
	got, err := s.ReadWork(t.Context(), task.ID)
	if err != nil || got.ID != task.ID || got.Machine != machine || s.state.Runtimes[task.ID] != "codex" || local.mutations.Load() != 0 {
		t.Fatal("address repair lost original execution route", got, err)
	}
	other := newDualPeerTarget(t)
	if _, err := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: machine, Address: other.http.URL, Name: renamed.Name}); err == nil || s.state.Profiles[machine].Peer.Address != next.URL {
		t.Fatal("different target replaced original peer")
	}
}

func TestEmptyBotProfileHasBoundedReadinessAndFreshness(t *testing.T) {
	target := newDualPeerTarget(t)
	s, _, _, machine, _ := dualPeerConnect(t, target)
	p := s.state.Profiles[machine]
	if p.View.CheckedAt == 0 || len(s.TaskMachines()) != 1 || !s.TaskMachines()[0].Ready {
		t.Fatal("pair did not publish fresh readiness")
	}
	p.View.CheckedAt = time.Now().Add(-61 * time.Second).UnixMilli()
	s.state.Profiles[machine] = p
	if s.TaskMachines()[0].Ready {
		t.Fatal("stale readiness advertised as current")
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { s.observeBotProfiles(ctx); close(done) }()
	waitPeerTest(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.state.Profiles[machine].View.CheckedAt > p.View.CheckedAt
	})
	cancel()
	<-done
	if !s.TaskMachines()[0].Ready || len(s.state.Routes) != 0 {
		t.Fatal("empty connection health required a Worker")
	}
	target.owner(s.state.ProfileID, "codex").connectFailure.Store(true)
	view, err := s.InspectMachine(t.Context(), machine, "")
	if err != nil || view.State != "setup" || view.Issue != "worker_not_ready" || view.CheckedAt == 0 {
		t.Fatal("native readiness not distinguished from unreachable transport", view, err)
	}
	if s.TaskMachines()[0].Ready {
		t.Fatal("unready empty connection stayed selectable")
	}
}

func TestBotReadinessDoesNotHoldRouteLockAndFencesStaleResults(t *testing.T) {
	target := newDualPeerTarget(t)
	var slow atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce sync.Once
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slow.Load() {
			enterOnce.Do(func() { close(entered) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			_ = json.NewEncoder(w).Encode(workerpeer.Response{Contract: api.WorkerContract, Peer: target.peer.Status().Peer, Error: "worker_not_ready: private fixture diagnostic"})
			return
		}
		target.peer.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	s, err := Open(t.TempDir(), &dualPeerLocal{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	code, err := target.peer.Pairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.ConnectBot(t.Context(), api.BotConnectionInput{Address: proxy.URL, Name: "Before", Code: code.Code})
	if err != nil {
		t.Fatal(err)
	}
	slow.Store(true)
	inspected := make(chan api.Machine, 1)
	go func() { view, _ := s.InspectMachine(t.Context(), first.ID, ""); inspected <- view }()
	<-entered
	renamed := make(chan error, 1)
	go func() {
		_, err := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: first.ID, Address: proxy.URL, Name: "After"})
		renamed <- err
	}()
	select {
	case err := <-renamed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("readiness held the global route lock")
	}
	close(release)
	if view := <-inspected; view.Name != "After" || view.State != "ready" || view.CheckedAt != first.CheckedAt {
		t.Fatal("stale readiness error overwrote a newer configuration", view)
	}
}

func TestArchivedRevokedBotRestoresSameProofAndRetainedTasks(t *testing.T) {
	target := newDualPeerTarget(t)
	s, m, _, machine, _ := dualPeerConnect(t, target)
	task, err := m.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "m1-retained-task", Title: "Retained", Prompt: "run"})
	if err != nil {
		t.Fatal(err)
	}
	proof := s.state.Profiles[machine].Peer
	if err = target.peer.Revoke(s.state.ProfileID); err != nil {
		t.Fatal(err)
	}
	if err = s.RemoveMachine(t.Context(), machine); err != nil || len(s.TaskMachines()) != 0 || len(s.Machines()) != 1 || !s.Machines()[0].Archived {
		t.Fatal("retained connection was deleted or stayed selectable", err)
	}
	if _, err = s.BindWork(t.Context(), api.TaskStart{Machine: machine}, "task-new-archived", ""); err == nil {
		t.Fatal("archived target accepted new work")
	}
	if _, err = s.ConnectBot(t.Context(), api.BotConnectionInput{ID: machine, Address: proof.Address, Name: "Archived rename"}); err != nil || !s.Machines()[0].Archived {
		t.Fatal("local rename implicitly restored archived authorization", err)
	}
	reopened, err := Open(s.root, &dualPeerLocal{}, nil)
	if err != nil || reopened.state.Profiles[machine].Peer != proof || reopened.state.Routes[task.ID] != machine {
		t.Fatal("archive lost sole original proof or task route", err)
	}
	code, err := target.peer.Pairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := reopened.ConnectBot(t.Context(), api.BotConnectionInput{Address: proof.Address, Name: "Restored", Code: code.Code})
	if err != nil || restored.ID != machine || restored.Archived || reopened.state.Profiles[machine].Peer.Token != proof.Token {
		t.Fatal("re-add did not restore original authenticated profile", restored, err)
	}
	if got, err := reopened.ReadWork(t.Context(), task.ID); err != nil || got.ID != task.ID || got.Status != "working" {
		t.Fatal("reauthorization lost original task", got, err)
	}
	if err = reopened.RemoveMachine(t.Context(), machine); err != nil {
		t.Fatal(err)
	}
	if restored, err = reopened.ConnectBot(t.Context(), api.BotConnectionInput{ID: machine, Address: proof.Address, Name: "Restored", Reconnect: true}); err != nil || restored.Archived {
		t.Fatal("explicit restore required pairing again", restored, err)
	}
	if err = target.peer.Revoke(reopened.state.ProfileID); err != nil {
		t.Fatal(err)
	}
	p := reopened.state.Profiles[machine]
	p.Peer.Token = ""
	reopened.state.Profiles[machine] = p
	code, err = target.peer.Pairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.ConnectBot(t.Context(), api.BotConnectionInput{ID: machine, Address: proof.Address, Code: code.Code, Reconnect: true}); err == nil || err.Error() != "original_peer_proof_required" {
		t.Fatal("fresh code bypassed missing original proof", err)
	}
}
