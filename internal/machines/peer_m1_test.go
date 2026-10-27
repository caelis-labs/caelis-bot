package machines

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
	go func() { s.observeMachineProfiles(ctx); close(done) }()
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
	target.ownerFor(s, "codex").connectFailure.Store(true)
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
	s, err := Open(t.TempDir(), &dualPeerLocal{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.ConnectBot(t.Context(), api.BotConnectionInput{Address: proxy.URL, Name: "Before"})
	if err != nil {
		t.Fatal(err)
	}
	approveMachine(t, target, s, first.ID)
	first = s.Machines()[0]
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

func TestDeletedMachineKeepsTaskIdentityUnavailableAfterReadd(t *testing.T) {
	target := newDualPeerTarget(t)
	s, m, _, machine, ledger := dualPeerConnect(t, target)
	task, err := m.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "original", Title: "Retained", Prompt: "run"})
	if err != nil {
		t.Fatal(err)
	}
	original := s.state.Profiles[machine]
	if err = s.RemoveMachine(t.Context(), machine); err != nil {
		t.Fatal(err)
	}
	if len(s.Machines()) != 0 || len(s.TaskMachines()) != 0 {
		t.Fatal("deleted connection retained")
	}
	reopened, err := Open(s.root, &dualPeerLocal{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.state.Routes[task.ID] != machine {
		t.Fatal("original route discarded")
	}
	fresh, err := reopened.ConnectBot(t.Context(), api.BotConnectionInput{Address: original.Peer.Address, Name: machineView(original).Name})
	if err != nil {
		t.Fatal(err)
	}
	approveMachine(t, target, reopened, fresh.ID)
	if fresh.ID == machine {
		t.Fatal("new identity reused")
	}
	if _, err = reopened.ReadWork(t.Context(), task.ID); err == nil {
		t.Fatal("old task reached a replacement machine")
	}
	if _, err = reopened.BindWork(t.Context(), api.TaskStart{Machine: fresh.ID}, task.ID, "codex"); err == nil {
		t.Fatal("old task rebound")
	}
	contents, err := os.ReadFile(ledger)
	if err != nil || !bytes.Contains(contents, []byte(task.ID)) {
		t.Fatal("task history erased", err)
	}
}
