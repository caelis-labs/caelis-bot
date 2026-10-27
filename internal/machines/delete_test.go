package machines

import (
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLegacyDeleteCleanupResumesOriginalReceiptAndPreservesOtherData(t *testing.T) {
	root := t.TempDir()
	v1 := []byte(`{"version":1,"profileId":"owner","extra":{"preserve":true},"profiles":{"old":{"key":"old-fixture-secret","view":{"id":"old","name":"fedora","address":"fedora"}},"other":{"privateField":"keep","view":{"id":"other","name":"other","address":"other"}}},"routes":{"historical-task":"old"},"runtimes":{"historical-task":"codex"}}`)
	if err := os.WriteFile(filepath.Join(root, "machines.json"), v1, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "old"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old", "known_hosts"), []byte("old host"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s, err := OpenWithCredentialCleanup(root, nil, func(id string) error {
		calls++
		if id != "old" {
			t.Fatal("another connection credential selected")
		}
		return errors.New("fixture denied")
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	input := api.MachineMutation{ID: "delete-original", MachineID: "old", Action: "remove"}
	r := s.ManageMachine(input)
	if r.Status != "checking" || !r.CleanupPending || calls != 1 {
		t.Fatal("cleanup failure reported success", r, calls)
	}
	if _, exists := s.state.Profiles["old"]; exists {
		t.Fatal("profile/proof not deleted")
	}
	// A new process resumes the original local cleanup, without a second removal.
	reopened, err := OpenWithCredentialCleanup(root, nil, func(id string) error { calls++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	r = reopened.MachineMutation(input.ID)
	if r.Status != "succeeded" || r.CleanupPending || calls != 2 {
		t.Fatal("cleanup not reconciled", r, calls)
	}
	if got := reopened.ManageMachine(input); got != r {
		t.Fatal("duplicate deletion changed outcome")
	}
	if _, err = os.Stat(filepath.Join(root, "old")); !os.IsNotExist(err) {
		t.Fatal("connection-owned known_hosts retained", err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "machines.json"))
	var d map[string]json.RawMessage
	json.Unmarshal(b, &d)
	var profiles map[string]json.RawMessage
	json.Unmarshal(d["profiles"], &profiles)
	if _, exists := profiles["old"]; exists || len(profiles) != 1 || string(d["extra"]) != "{\"preserve\":true}" {
		t.Fatal("legacy cleanup changed unrelated data", string(b))
	}
	if reopened.state.Routes["historical-task"] != "old" || reopened.state.Runtimes["historical-task"] != "codex" {
		t.Fatal("original task route lost")
	}
	fresh, err := reopened.PrepareBotConnection(api.BotConnectionInput{Name: "fedora", Address: "ssh://fedora:9317"}, nil)
	if err != nil || fresh.ID == "old" {
		t.Fatal("name not released", err)
	}
}

func TestArchivedMigrationDeletesOnlyExplicitlyRemovedProfiles(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.state.Profiles["old"] = profile{Label: "fedora", View: api.Machine{ID: "old", Name: "fedora", Archived: true}, Peer: workerpeer.Connection{Token: "old-proof"}}
	s.state.Profiles["live"] = profile{Label: "live", View: api.Machine{ID: "live", Name: "live"}, Peer: workerpeer.Connection{Token: "live-proof"}}
	s.state.Routes["history"] = "old"
	s.state.Runtimes["history"] = "codex"
	if err = s.save(); err != nil {
		t.Fatal(err)
	}
	s.Close()
	reopened, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if len(reopened.Machines()) != 1 || reopened.state.Profiles["live"].Peer.Token != "live-proof" || reopened.state.Routes["history"] != "old" {
		t.Fatal("archive upgrade lost unrelated identity")
	}
	if _, err = reopened.PrepareBotConnection(api.BotConnectionInput{ID: "old", Address: "http://127.0.0.1:9317"}, nil); err == nil {
		t.Fatal("deleted old ID resurrected")
	}
}

func TestDeleteDuringPairingFencesLateReceiptAndReleasesName(t *testing.T) {
	target := newDualPeerTarget(t)
	entered, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered); <-resume })
		target.peer.ServeHTTP(w, r)
	}))
	defer gateway.Close()
	s, err := Open(t.TempDir(), inert{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	in := api.BotConnectionInput{Name: "fedora", Address: gateway.URL}
	ref, err := s.PrepareBotConnection(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	go func() { _, _, e := s.ConnectPreparedBot(t.Context(), ref, in); completed <- e }()
	<-entered
	result := s.ManageMachine(api.MachineMutation{ID: "delete-during-connect", MachineID: ref.ID, Action: "remove"})
	close(resume)
	if result.Status != "succeeded" {
		t.Fatal(result)
	}
	if err = <-completed; err == nil {
		t.Fatal("late connect reported success")
	}
	if len(s.Machines()) != 0 || len(target.peer.Status().Requests) != 0 {
		t.Fatal("late identity read resurrected or paired removed connection")
	}
	if _, _, err = s.ConnectPreparedBot(t.Context(), ref, in); err == nil {
		t.Fatal("old prepared reference admitted")
	}
	if _, err = s.ConnectBot(t.Context(), api.BotConnectionInput{ID: ref.ID, Address: gateway.URL}); err == nil {
		t.Fatal("delayed old ID recreated connection")
	}
	fresh, err := s.PrepareBotConnection(in, nil)
	if err != nil || fresh.ID == ref.ID {
		t.Fatal("same-name new connection rejected or reused old identity", err)
	}
	if got := s.ManageMachine(api.MachineMutation{ID: "late-save", MachineID: ref.ID, Action: "save", Name: "fedora"}); got.Code != "machine_unknown" {
		t.Fatal("late edit recreated connection", got)
	}
}

func TestFailedDeletePublicationDoesNotStartCleanup(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.state.Profiles["original"] = profile{View: api.Machine{ID: "original", Name: "fedora"}, Label: "fedora"}
	originalPath := s.registry
	block := filepath.Join(s.root, "blocked")
	if err = os.WriteFile(block, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s.registry = filepath.Join(block, "state.json")
	r := s.ManageMachine(api.MachineMutation{ID: "failed-delete", MachineID: "original", Action: "remove"})
	if r.Status != "failed" || r.CleanupPending {
		t.Fatal(r)
	}
	if _, ok := s.state.Profiles["original"]; !ok {
		t.Fatal("failed delete changed state")
	}
	s.registry = originalPath
	if got := s.MachineMutation(r.ID); got.Status != "failed" || got.CleanupPending {
		t.Fatal(got)
	}
}
