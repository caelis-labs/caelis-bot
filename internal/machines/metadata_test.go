package machines

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestLegacyEditsPreserveBytesAndDeletionClearsOnlyOriginalConnection(t *testing.T) {
	root := t.TempDir()
	original := []byte(`{"version":1,"profileId":"original-profile","profiles":{"old":{"legacyCredential":"fixture-only-proof","view":{"id":"old","name":"fedora","address":"fedora","state":"ready"}}},"routes":{"original-task":"old"},"runtimes":{"original-task":"codex"}}`)
	if err := os.WriteFile(filepath.Join(root, "machines.json"), original, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := s.InspectMachine(t.Context(), "old", "")
	if err != nil || v.Name != "fedora" || v.State != "legacy" || v.Issue == "pairing_request_not_dispatched" {
		t.Fatal(v, err)
	}
	if result := s.ManageMachine(api.MachineMutation{ID: "rename", MachineID: "old", Action: "save", Name: "old-fedora"}); result.Status != "succeeded" {
		t.Fatal(result)
	}
	after, err := os.ReadFile(filepath.Join(root, "machines.json"))
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("edit changed v1 source", err)
	}
	if result := s.ManageMachine(api.MachineMutation{ID: "hide", MachineID: "old", Action: "remove"}); result.Status != "succeeded" {
		t.Fatal(result)
	}
	after, err = os.ReadFile(filepath.Join(root, "machines.json"))
	if err != nil || bytes.Contains(after, []byte("fixture-only-proof")) {
		t.Fatal("deleted legacy credential retained", err)
	}
	reopened, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if len(reopened.state.Profiles) != 0 || reopened.state.Routes["original-task"] != "old" || reopened.state.Runtimes["original-task"] != "codex" {
		t.Fatal("deleted profile survived or history route changed")
	}
	if r := reopened.MachineMutation("hide"); r.Status != "succeeded" || r.Machine.ID != "old" {
		t.Fatal(r)
	}
	// Even an absent companion cannot re-import a removed v1 profile.
	if err = os.Remove(filepath.Join(root, "machines-v2.json")); err != nil {
		t.Fatal(err)
	}
	legacy, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if len(legacy.Machines()) != 0 || legacy.state.Routes["original-task"] != "old" {
		t.Fatal("legacy source resurrected deletion")
	}

}
func TestLocalEditsKeepPairingIntentAndReturnOriginalReceipt(t *testing.T) {
	target := newDualPeerTarget(t)
	s, _, _, id, _ := dualPeerConnect(t, target)
	old := s.state.Profiles[id]
	target.http.Close()
	in := api.MachineMutation{ID: "edit-offline", MachineID: id, Action: "save", Name: "short-name", Address: "http://127.0.0.1:19329"}
	r := s.ManageMachine(in)
	if r.Status != "succeeded" || r.Machine.SavedAddress != in.Address {
		t.Fatal(r)
	}
	if s.state.Profiles[id].Peer != old.Peer || s.state.Profiles[id].PairingEpoch != old.PairingEpoch || s.state.Profiles[id].View.Name != old.View.Name {
		t.Fatal("local save rewrote pairing intent")
	}
	if again := s.ManageMachine(in); again != r {
		t.Fatal("duplicate operation did not return original receipt")
	}
	if got := s.ManageMachine(api.MachineMutation{ID: "hide-offline", MachineID: id, Action: "remove"}); got.Status != "succeeded" {
		t.Fatal(got)
	}
	if _, exists := s.state.Profiles[id]; exists {
		t.Fatal("removal retained proof")
	}
	r.Machine = removedMachine(id) // Keep outcome/digest; redact deleted configuration from old receipts.
	if got := s.MachineMutation(in.ID); got != r {
		t.Fatal("deletion changed original outcome/digest")
	}
	b, _ := os.ReadFile(s.registry)
	var value disk
	if json.Unmarshal(b, &value) != nil || value.Mutations[in.ID] != r {
		t.Fatal("receipt not durable")
	}
}
func TestLocalMutationDefiniteFailureDoesNotClaimUnknown(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.state.Profiles["one"] = profile{Label: "one", View: api.Machine{ID: "one", Name: "one"}}
	s.registry = filepath.Join(s.root, "blocked", "state.json")
	if err := os.WriteFile(filepath.Join(s.root, "blocked"), []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	r := s.ManageMachine(api.MachineMutation{ID: "fail", MachineID: "one", Action: "remove"})
	if r.Status != "failed" || s.state.Profiles["one"].View.Archived {
		t.Fatal(r)
	}
}

func TestUnknownLocalMutationOnlyAdoptsOriginalDurableRegistry(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.state.Profiles["one"] = profile{Label: "one", View: api.Machine{ID: "one", Name: "one"}}
	input := api.MachineMutation{ID: "original-save", MachineID: "one", Action: "save", Name: "renamed"}
	receipt := s.ManageMachine(input)
	if receipt.Status != "succeeded" {
		t.Fatal(receipt)
	}
	original, err := os.ReadFile(s.registry)
	if err != nil {
		t.Fatal(err)
	}
	unknown := receipt
	unknown.Status = "unknown"
	s.mutations[input.ID] = unknown
	s.loadErr = errors.New("machine_save_unknown")
	var unrelated disk
	if err = json.Unmarshal(original, &unrelated); err != nil {
		t.Fatal(err)
	}
	unrelated.ProfileID = "unrelated-owner"
	other, _ := json.Marshal(unrelated)
	if err = os.WriteFile(s.registry, other, 0600); err != nil {
		t.Fatal(err)
	}
	if got := s.MachineMutation(input.ID); got.Status != "unknown" {
		t.Fatal("unrelated receipt accepted", got)
	}
	s.Machines()
	if s.loadErr == nil {
		t.Fatal("ordinary list read cleared unknown publication fence")
	}
	if got := s.ManageMachine(api.MachineMutation{ID: "blocked", MachineID: "one", Action: "remove"}); got.Code != "machine_store_unavailable" {
		t.Fatal(got)
	}
	if err = os.WriteFile(s.registry, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := s.MachineMutation(input.ID); got.Status != "unknown" {
		t.Fatal("corrupt store reported failure/success", got)
	}
	if err = os.WriteFile(s.registry, original, 0600); err != nil {
		t.Fatal(err)
	}
	if got := s.MachineMutation(input.ID); got != receipt || s.loadErr != nil {
		t.Fatal("original receipt not recovered", got, s.loadErr)
	}
}
func TestReopenedMutationRejectsIDCollisionWithoutReplacingReceipt(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.state.Profiles["one"] = profile{Label: "one", View: api.Machine{ID: "one", Name: "one"}}
	input := api.MachineMutation{ID: "same-id", MachineID: "one", Action: "save", Name: "renamed"}
	original := s.ManageMachine(input)
	s.Close()
	reopened, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	different := input
	different.Name = "must-not-apply"
	if got := reopened.ManageMachine(different); got.Status != "failed" || got.Code != "machine_change_invalid" {
		t.Fatal(got)
	}
	if got := reopened.MachineMutation(input.ID); got != original {
		t.Fatal("original receipt replaced", got)
	}
	if got := reopened.ManageMachine(input); got != original {
		t.Fatal("original retry changed", got)
	}
}
