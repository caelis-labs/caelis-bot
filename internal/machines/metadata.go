package machines

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"
	"syscall"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func (s *Service) ManageMachine(in api.MachineMutation) api.MachineMutationResult {
	raw, _ := json.Marshal(in)
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	result := api.MachineMutationResult{ID: in.ID, Status: "failed", Code: "machine_change_invalid", Digest: digest}
	if in.ID == "" || len(in.ID) > 128 {
		return result
	}
	s.mutationMu.Lock()
	if old, ok := s.mutations[in.ID]; ok {
		s.mutationMu.Unlock()
		if old.Digest != digest {
			return result
		}
		return old
	}
	s.mutations[in.ID] = api.MachineMutationResult{ID: in.ID, Status: "checking", Digest: digest}
	s.mutationMu.Unlock()
	defer func() {
		s.mutationMu.Lock()
		if old, ok := s.mutations[in.ID]; !ok || old.Status == "checking" || old.Digest == digest {
			s.mutations[in.ID] = result
		}
		s.mutationMu.Unlock()
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.state.Mutations[in.ID]; ok {
		if old.Digest == digest {
			result = old
		} else {
			s.mutationMu.Lock()
			s.mutations[in.ID] = old
			s.mutationMu.Unlock()
		}
		return result
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
		p.View.Archived = true
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
	s.state.Mutations[in.ID] = result
	if err := localstate.WriteConfirmed(s.registry, s.state); err != nil {
		// Directory sync can fail after rename. Verify the exact original receipt on
		// disk before classifying; a cached Machines() projection cannot prove a write.
		diskState, readErr := s.readMutationRegistryLocked()
		if readErr == nil && diskState.Mutations[in.ID].Digest == digest && diskState.Mutations[in.ID].Status == "succeeded" {
			if localstate.SyncParent(s.registry) == nil {
				return result
			}
			readErr = errors.New("machine_save_unknown")
		}
		s.state = before
		result.Status = "failed"
		result.Code = "machine_save_failed"
		if readErr != nil && !os.IsNotExist(readErr) && !errors.Is(readErr, syscall.ENOTDIR) {
			result.Status = "unknown"
			result.Code = "machine_save_unknown"
			s.loadErr = errors.New("machine_save_unknown")
		}
	}
	return result
}
func (s *Service) MachineMutation(id string) api.MachineMutationResult {
	s.mutationMu.Lock()
	r, ok := s.mutations[id]
	s.mutationMu.Unlock()
	if ok && r.Status != "unknown" {
		return r
	}
	// Persisted readback is authoritative even after reopening the app. Never
	// adopt an unrelated/corrupt registry or publish success without directory sync.
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := s.readMutationRegistryLocked()
	if err == nil {
		if receipt, found := value.Mutations[id]; found && receipt.ID == id && receipt.Status == "succeeded" && (!ok || r.Digest == receipt.Digest) {
			if localstate.SyncParent(s.registry) != nil {
				return api.MachineMutationResult{ID: id, Status: "unknown", Code: "machine_save_unknown"}
			}
			s.resolveMutationLocked(id, receipt, value)
			return receipt
		}
		if ok && r.Status == "unknown" && value.Mutations[id].ID == "" {
			r.Status = "failed"
			r.Code = "machine_save_failed"
			s.resolveMutationLocked(id, r, value)
			return r
		}
	}
	return api.MachineMutationResult{ID: id, Status: "unknown", Code: "machine_save_unknown"}
}
func (s *Service) readMutationRegistryLocked() (disk, error) {
	b, err := os.ReadFile(s.registry)
	var value disk
	if err != nil {
		return value, err
	}
	if json.Unmarshal(b, &value) != nil || value.Version != 2 || value.ProfileID != s.state.ProfileID || value.Profiles == nil || value.Routes == nil || value.Runtimes == nil {
		return value, errors.New("machine_store_invalid")
	}
	if value.Mutations == nil {
		value.Mutations = map[string]api.MachineMutationResult{}
	}
	if value.Reservations == nil {
		value.Reservations = map[string]bool{}
	}
	return value, nil
}
func (s *Service) resolveMutationLocked(id string, r api.MachineMutationResult, value disk) {
	s.mutationMu.Lock()
	s.mutations[id] = r
	s.mutationMu.Unlock()
	if s.loadErr != nil && s.loadErr.Error() == "machine_save_unknown" {
		s.state = value
		s.loadErr = nil
	}
}
