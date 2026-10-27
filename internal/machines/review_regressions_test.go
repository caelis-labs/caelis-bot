package machines

import (
	"bytes"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestMissingMutationReadbackFencesLateDelivery(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.state.Profiles["one"] = profile{Label: "one", View: api.Machine{ID: "one", Name: "one"}}
	if err = s.save(); err != nil {
		t.Fatal(err)
	}
	r := s.MachineMutation("not-delivered")
	if s.state.Reservations == nil || s.state.ReservationKeys == nil {
		t.Fatal("task reservation maps lost")
	}
	if r.Status != "failed" {
		t.Fatalf("undelivered operation cannot finish: %+v", r)
	}
	late := s.ManageMachine(api.MachineMutation{ID: r.ID, MachineID: "one", Action: "remove"})
	if late.Status != "failed" || s.state.Profiles["one"].View.Archived {
		t.Fatal("late delivery applied after terminal readback", late)
	}
	reopened, err := Open(s.root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.MachineMutation(r.ID); got.Status != "failed" {
		t.Fatal("terminal fence lost after restart", got)
	}
	if got := reopened.ManageMachine(api.MachineMutation{ID: r.ID, MachineID: "one", Action: "remove"}); got.Status != "failed" || reopened.state.Profiles["one"].View.Archived {
		t.Fatal("late delivery after restart applied", got)
	}
}
func TestRejectedMutationReceiptSurvivesRestart(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.state.Profiles["one"] = profile{Label: "one", View: api.Machine{ID: "one", Name: "one"}}
	if err = s.save(); err != nil {
		t.Fatal(err)
	}
	in := api.MachineMutation{ID: "rejected", MachineID: "one", Action: "save", Name: ""}
	r := s.ManageMachine(in)
	if r.Status != "failed" {
		t.Fatal(r)
	}
	reopened, err := Open(s.root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.MachineMutation(in.ID); got != r {
		t.Fatal("lost rejection after restart", got, r)
	}
}
func TestSavedAddressConsumedAfterSamePeerRepair(t *testing.T) {
	target := newDualPeerTarget(t)
	s, _, _, id, _ := dualPeerConnect(t, target)
	original := s.state.Profiles[id]
	b := httptest.NewServer(target.peer)
	defer b.Close()
	c := httptest.NewServer(target.peer)
	defer c.Close()
	if r := s.ManageMachine(api.MachineMutation{ID: "save-b", MachineID: id, Action: "save", Name: machineView(original).Name, Address: b.URL}); r.Status != "succeeded" {
		t.Fatal(r)
	}
	for _, address := range []string{b.URL, c.URL} {
		v, err := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: id, Address: address, Name: machineView(original).Name})
		if err != nil || v.Address != address || v.SavedAddress != "" || s.state.Profiles[id].SavedAddress != "" {
			t.Fatalf("stale candidate after connect: view=%+v saved=%s err=%v", v, s.state.Profiles[id].SavedAddress, err)
		}
	}
	p := s.state.Profiles[id]
	if p.Peer.Token != original.Peer.Token || p.Peer.Peer != original.Peer.Peer || p.PairingEpoch != original.PairingEpoch {
		t.Fatal("original proof changed")
	}
	reopened, err := Open(s.root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if p := reopened.state.Profiles[id]; p.SavedAddress != "" || p.View.Address != c.URL {
		t.Fatal("candidate resurrected", p.SavedAddress, p.View.Address)
	}
}

func TestMutationReadbackAndAdmissionHaveOneTerminalResult(t *testing.T) {
	for i := 0; i < 20; i++ {
		s, err := Open(t.TempDir(), nil)
		if err != nil {
			t.Fatal(err)
		}
		s.state.Profiles["one"] = profile{Label: "one", View: api.Machine{ID: "one", Name: "one"}}
		if err = s.save(); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var write, read api.MachineMutationResult
		go func() {
			defer wg.Done()
			<-start
			write = s.ManageMachine(api.MachineMutation{ID: "racing", MachineID: "one", Action: "remove"})
		}()
		go func() { defer wg.Done(); <-start; read = s.MachineMutation("racing") }()
		close(start)
		wg.Wait()
		final := s.MachineMutation("racing")
		if final.Status == "succeeded" {
			if write.Status != "succeeded" || !s.state.Profiles["one"].View.Archived || read.Status == "failed" {
				t.Fatal(write, read, final)
			}
		} else if final.Status == "failed" {
			if write.Status != "failed" || s.state.Profiles["one"].View.Archived {
				t.Fatal(write, read, final)
			}
		} else {
			t.Fatal(final)
		}
		s.Close()
	}
}
func TestApplyingCandidateRetainsNewerSavedIntent(t *testing.T) {
	target := newDualPeerTarget(t)
	s, _, _, id, _ := dualPeerConnect(t, target)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered); <-release })
		target.peer.ServeHTTP(w, r)
	}))
	defer b.Close()
	name := machineView(s.state.Profiles[id]).Name
	if r := s.ManageMachine(api.MachineMutation{ID: "save-b", MachineID: id, Action: "save", Name: name, Address: b.URL}); r.Status != "succeeded" {
		t.Fatal(r)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: id, Address: b.URL, Name: name})
		done <- err
	}()
	<-entered
	if r := s.ManageMachine(api.MachineMutation{ID: "save-c", MachineID: id, Action: "save", Name: "new-name", Address: "http://127.0.0.1:19329"}); r.Status != "succeeded" {
		t.Fatal(r)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	p := s.state.Profiles[id]
	if p.View.Address != b.URL || p.SavedAddress != "http://127.0.0.1:19329" || p.Label != "new-name" {
		t.Fatal("newer intent lost", p.SavedAddress, p.Label)
	}
}
func TestArchivedSSHReclamationProtectsAllReferences(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	add := func(id, address string, archived bool) {
		s.state.Profiles[id] = profile{Peer: workerpeer.Connection{Address: address}, View: api.Machine{ID: id, Address: address, Archived: archived}}
	}
	add("unused", "ssh://unused:9317", true)
	add("routed", "ssh://routed:9317", true)
	s.state.Routes["history"] = "routed"
	add("shared-archived", "ssh://shared:9317", true)
	add("shared-active", "ssh://shared:0", false)
	add("in-flight", "ssh://checking:9317", true)
	s.pairingActive = map[string]int{"in-flight": 1}
	add("selected", "ssh://selected:9317", true)
	got := s.unusedArchivedSSHLocked("ssh://selected:9317")
	if len(got) != 1 || got[0] != "ssh://unused:9317" {
		t.Fatal(got)
	}
}

func TestUndeliveredFirstLegacyEditFencesWithoutReplacingV1(t *testing.T) {
	root := t.TempDir()
	original := []byte(`{"version":1,"profileId":"original","profiles":{"old":{"view":{"id":"old","name":"fedora","address":"fedora"}}},"routes":{},"runtimes":{}}`)
	path := filepath.Join(root, "machines.json")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.MachineMutation("first-lost"); got.Status != "failed" {
		t.Fatal(got)
	}
	if got := s.ManageMachine(api.MachineMutation{ID: "first-lost", MachineID: "old", Action: "remove"}); got.Status != "failed" || s.state.Profiles["old"].View.Archived {
		t.Fatal(got)
	}
	b, _ := os.ReadFile(path)
	if !bytes.Equal(b, original) {
		t.Fatal("original v1 changed")
	}
	reopened, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.MachineMutation("first-lost"); got.Status != "failed" {
		t.Fatal(got)
	}
	if err = os.Remove(s.registry); err != nil {
		t.Fatal(err)
	}
	if got := s.MachineMutation("deleted-registry"); got.Status != "unknown" {
		t.Fatal("adopted v1 after v2 publication", got)
	}
}
