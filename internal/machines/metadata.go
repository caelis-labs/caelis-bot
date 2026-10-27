package machines

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func (s *Service) ManageMachine(in api.MachineMutation) (result api.MachineMutationResult) {
	raw, _ := json.Marshal(in)
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	result = api.MachineMutationResult{ID: in.ID, Status: "failed", Code: "machine_change_invalid", Digest: digest}
	if in.ID == "" || len(in.ID) > 128 {
		return result
	}
	s.mutationMu.Lock()
	if old, ok := s.mutations[in.ID]; ok {
		s.mutationMu.Unlock()
		if old.Code == "machine_not_submitted" {
			return old
		}
		if old.Digest != digest {
			return result
		}
		return s.MachineMutation(in.ID)
	}
	s.mutations[in.ID] = api.MachineMutationResult{ID: in.ID, Status: "checking", Digest: digest}
	s.mutationMu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.mutationMu.Lock()
		if old, ok := s.mutations[in.ID]; !ok || old.Status == "checking" || old.Digest == digest {
			s.mutations[in.ID] = result
		}
		for id, r := range s.mutations {
			if _, durable := s.state.Mutations[id]; !durable && r.Status != "checking" && !r.CleanupPending && mutationExpired(id, s.state.MutationFloor) {
				delete(s.mutations, id)
			}
		}
		s.mutationMu.Unlock()
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		// Rejections are receipts too: a lost response must survive restart.
		if result.Status == "failed" && s.loadErr == nil {
			if _, exists := s.state.Mutations[in.ID]; !exists {
				if err := s.persistMutationReceiptLocked(result); err != nil {
					// No effect occurred; retain the failure in memory. A later
					// readback fences an absent ID durably before permitting retry.
					return
				}
			}
		}
	}()
	if old, ok := s.state.Mutations[in.ID]; ok {
		if old.Code == "machine_not_submitted" {
			result = old
			return result
		}
		if old.Digest == digest {
			result = old
		} else {
			s.mutationMu.Lock()
			s.mutations[in.ID] = old
			s.mutationMu.Unlock()
		}
		return result
	}
	if mutationExpired(in.ID, s.state.MutationFloor) {
		return expiredMutation(in.ID)
	}
	result.IssuedAt = time.Now().UnixMilli()
	if stamp, valid := mutationTime(in.ID); valid {
		result.IssuedAt = stamp
	}
	if s.loadErr != nil {
		result.Code = "machine_store_unavailable"
		return result
	}
	p, ok := s.state.Profiles[in.MachineID]
	if !ok {
		result.Code = "machine_unknown"
		return result
	}
	switch in.Action {
	case "remove":
		// Delete the profile below, together with the operation receipt.
	case "save":
		name := strings.TrimSpace(in.Name)
		if !validPeerDisplayName(name) {
			result.Code = "invalid_bot_name"
			return result
		}
		if !s.labelAvailableLocked(in.MachineID, name) {
			result.Code = "bot_name_in_use"
			return result
		}
		if in.Address != "" && in.Address != p.View.Address {
			address, err := botEndpoint(in.Address)
			if err != nil {
				result.Code = "invalid_bot_address"
				return result
			}
			p.SavedAddress = address
		} else {
			p.SavedAddress = ""
		}
		p.Label = name
		p.LabelPending = false
		// Pairing intent keeps its original name; Label is local only.
	default:
		return result
	}
	// The receipt and local effect are one atomic publication. A lost renderer
	// response is resolved by this ID, not by replaying a network operation.
	before := s.state
	s.state.Profiles = maps.Clone(s.state.Profiles)
	s.state.Mutations = maps.Clone(s.state.Mutations)
	s.state.Profiles[in.MachineID] = p
	result.Status = "succeeded"
	result.Code = ""
	result.Machine = machineView(p)
	if in.Action == "remove" {
		delete(s.state.Profiles, in.MachineID)
		s.state.Reservations = maps.Clone(s.state.Reservations)
		s.state.ReservationKeys = maps.Clone(s.state.ReservationKeys)
		for task, machine := range s.state.Routes {
			if machine == in.MachineID {
				delete(s.state.Reservations, task)
				delete(s.state.ReservationKeys, task)
			}
		}
		result.Machine = removedMachine(in.MachineID)
		result.Status = "checking"
		result.CleanupPending = true
		// Keep operation IDs/outcomes/digests, not copies of deleted configuration.
		for id, receipt := range s.state.Mutations {
			if receipt.Machine.ID == in.MachineID {
				receipt.Machine = removedMachine(in.MachineID)
				s.state.Mutations[id] = receipt
			}
		}
	}
	s.state.Mutations[in.ID] = result
	s.compactMutationReceiptsLocked(in.ID)
	if err := localstate.WriteConfirmed(s.registry, s.state); err != nil {
		// Directory sync can fail after rename. Verify the exact original receipt on
		// disk before classifying; a cached Machines() projection cannot prove a write.
		diskState, readErr := s.readMutationRegistryLocked()
		if readErr == nil && diskState.Mutations[in.ID].Digest == digest && diskState.Mutations[in.ID].Status == result.Status {
			if localstate.SyncParent(s.registry) == nil {
				s.legacyUnpublished = false
				if result.CleanupPending {
					s.forgetDeletedRuntimeLocked(p)
					result = s.finishDeletionLocked(result)
				}
				return result
			}
			readErr = errors.New("machine_save_unknown")
		}
		s.state = before
		result.Status = "failed"
		result.CleanupPending = false
		result.Code = "machine_save_failed"
		if readErr != nil && !os.IsNotExist(readErr) && !errors.Is(readErr, syscall.ENOTDIR) {
			result.Status = "unknown"
			result.Code = "machine_save_unknown"
			s.loadErr = errors.New("machine_save_unknown")
		}
	}
	if result.Status == "succeeded" || result.CleanupPending && result.Status == "checking" {
		s.legacyUnpublished = false
		if result.CleanupPending {
			s.forgetDeletedRuntimeLocked(p)
			result = s.finishDeletionLocked(result)
		}
	}
	return result
}

// A missing ID can only become terminal while admission and publication are
// excluded. The durable fence rejects a delayed original ManageMachine call.
// This is local receipt reconciliation, never replay of a remote operation.
func (s *Service) MachineMutation(id string) api.MachineMutationResult {
	unknown := api.MachineMutationResult{ID: id, Status: "unknown", Code: "machine_save_unknown"}
	if id == "" || len(id) > 128 {
		return unknown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	r, ok := s.mutations[id]
	if ok && r.Status != "unknown" && (!mutationExpired(id, s.state.MutationFloor) || s.state.Mutations[id].ID == id) {
		if stored, exists := s.state.Mutations[id]; exists {
			r = stored
		}
		if r.CleanupPending {
			r = s.finishDeletionLocked(r)
			s.mutations[id] = r
		}
		return r
	}
	value, err := s.readMutationRegistryLocked()
	if err != nil {
		return unknown
	}
	if receipt, found := value.Mutations[id]; found {
		if receipt.ID != id || (ok && r.Digest != receipt.Digest) || (receipt.Status != "succeeded" && receipt.Status != "failed" && !receipt.CleanupPending) {
			return unknown
		}
		if localstate.SyncParent(s.registry) != nil {
			return unknown
		}
		if receipt.CleanupPending {
			before := s.state
			s.state = value
			if p, exists := before.Profiles[receipt.Machine.ID]; exists {
				s.forgetDeletedRuntimeLocked(p)
			}
			receipt = s.finishDeletionLocked(receipt)
		}
		s.mutations[id] = receipt
		if s.loadErr != nil && s.loadErr.Error() == "machine_save_unknown" {
			if !receipt.CleanupPending {
				s.state, _ = s.readMutationRegistryLocked()
			}
			s.loadErr = nil
		}
		return receipt
	}
	if mutationExpired(id, value.MutationFloor) {
		return expiredMutation(id)
	}
	if s.loadErr != nil && s.loadErr.Error() != "machine_save_unknown" {
		return unknown
	}
	// No in-flight admission exists under mutationMu; sync the original registry
	// and publish the fence before reporting that this operation did not commit.
	r = api.MachineMutationResult{ID: id, Status: "failed", Code: "machine_not_submitted", IssuedAt: time.Now().UnixMilli()}
	if stamp, valid := mutationTime(id); valid {
		r.IssuedAt = stamp
	}
	if ok {
		r.Digest = s.mutations[id].Digest
	}
	before := s.state
	s.state = value
	if err := s.persistMutationReceiptLocked(r); err != nil {
		s.state = before
		return unknown
	}
	s.mutations[id] = r
	if s.loadErr != nil && s.loadErr.Error() == "machine_save_unknown" {
		s.loadErr = nil
	}
	return r
}
func (s *Service) persistMutationReceiptLocked(r api.MachineMutationResult) error {
	before := s.state.Mutations
	beforeFloor := s.state.MutationFloor
	s.state.Mutations = maps.Clone(before)
	s.state.Mutations[r.ID] = r
	s.compactMutationReceiptsLocked(r.ID)
	if err := localstate.WriteConfirmed(s.registry, s.state); err != nil {
		value, readErr := s.readMutationRegistryLocked()
		if readErr == nil && value.Mutations[r.ID] == r && localstate.SyncParent(s.registry) == nil {
			s.legacyUnpublished = false
			return nil
		}
		s.state.Mutations = before
		s.state.MutationFloor = beforeFloor
		return err
	}
	s.legacyUnpublished = false
	return nil
}
func (s *Service) readMutationRegistryLocked() (disk, error) {
	b, err := os.ReadFile(s.registry)
	legacy := os.IsNotExist(err) && s.legacyUnpublished
	if legacy {
		b, err = os.ReadFile(filepath.Join(s.root, "machines.json"))
	}
	var value disk
	if err != nil {
		return value, err
	}
	if json.Unmarshal(b, &value) != nil || (!legacy && value.Version != 2) || (legacy && value.Version != 1) || value.ProfileID != s.state.ProfileID || value.Profiles == nil || value.Routes == nil || value.Runtimes == nil {
		return value, errors.New("machine_store_invalid")
	}
	if legacy {
		value = migratedLegacy(value)
	}
	if value.Mutations == nil {
		value.Mutations = map[string]api.MachineMutationResult{}
	}
	if value.Reservations == nil {
		value.Reservations = map[string]bool{}
	}
	if value.ReservationKeys == nil {
		value.ReservationKeys = map[string]string{}
	}
	return value, nil
}
