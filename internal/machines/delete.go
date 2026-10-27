package machines

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// A receipt carries the original operation identity, never a second connection.
func removedMachine(id string) api.Machine {
	return api.Machine{ID: id, State: "removed", Archived: true}
}

// The profile and its proof are already absent from the authoritative registry.
// Resume only idempotent, local cleanup through the original operation receipt.
// Publishing succeeded before this step would conceal retained v1 credentials.
func (s *Service) finishDeletionLocked(r api.MachineMutationResult) api.MachineMutationResult {
	if !r.CleanupPending {
		return r
	}
	if _, exists := s.state.Profiles[r.Machine.ID]; exists {
		r.Status = "unknown"
		r.Code = "machine_save_unknown"
		return r
	}
	if err := s.cleanLegacyConnectionLocked(r.Machine.ID); err != nil {
		r.Status = "checking"
		r.Code = "machine_delete_cleanup"
		return r
	}
	finished := r
	finished.Status = "succeeded"
	finished.Code = ""
	finished.CleanupPending = false
	if err := s.persistMutationReceiptLocked(finished); err != nil {
		r.Status = "unknown"
		r.Code = "machine_save_unknown"
		return r
	}
	return finished
}

func (s *Service) cleanLegacyConnectionLocked(id string) error {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return errors.New("machine_delete_invalid")
	}
	path := filepath.Join(s.root, "machines.json")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var header struct {
		Version   int    `json:"version"`
		ProfileID string `json:"profileId"`
	}
	if json.Unmarshal(b, &header) != nil {
		return errors.New("machine_legacy_store_invalid")
	}
	if header.Version != 1 {
		return nil
	}
	if header.ProfileID != s.state.ProfileID {
		return errors.New("machine_legacy_owner_changed")
	}
	// Preserve unrelated profiles, original task routes, and unknown legacy fields.
	var document map[string]json.RawMessage
	var profiles map[string]json.RawMessage
	if json.Unmarshal(b, &document) != nil || json.Unmarshal(document["profiles"], &profiles) != nil {
		return errors.New("machine_legacy_store_invalid")
	}
	if _, exists := profiles[id]; !exists {
		return nil
	}
	if s.deleteLegacyCredential != nil {
		if err = s.deleteLegacyCredential(id); err != nil {
			return err
		}
	}
	dir := filepath.Join(s.root, id)
	if info, statErr := os.Lstat(dir); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("machine_legacy_directory_invalid")
		}
		if err = os.Remove(filepath.Join(dir, "known_hosts")); err != nil && !os.IsNotExist(err) {
			return err
		}
		// Never recursively remove an unexpected file or another task's data.
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			return readErr
		}
		if len(entries) == 0 {
			if err = os.Remove(dir); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	delete(profiles, id)
	document["profiles"], err = json.Marshal(profiles)
	if err != nil {
		return err
	}
	return localstate.WriteConfirmed(path, document)
}

func (s *Service) forgetDeletedRuntimeLocked(p profile) {
	s.cacheMu.Lock()
	for id, machine := range s.state.Routes {
		if machine != p.View.ID {
			continue
		}
		delete(s.observations, id)
		delete(s.observationAt, id)
	}
	for key := range s.cache {
		if strings.HasPrefix(key, p.View.ID+"\x00") || key == p.View.ID {
			delete(s.cache, key)
			s.cacheRevision[key]++
		}
	}
	s.cacheMu.Unlock()
	// Task routes are intentionally retained: absence must never route to local or
	// a newly connected machine with the same name. Reservations cannot dispatch.
	s.releaseSSHIfUnusedLocked(sshChannelAddress(p.View.Address, p.Legacy))
	s.releaseSSHIfUnusedLocked(p.Peer.Address)
	s.releaseSSHIfUnusedLocked(p.SavedAddress)
}

func (s *Service) releaseSSHIfUnusedLocked(address string) {
	address = sshChannelAddress(address, false)
	if address == "" || s.sshUses[address] > 0 {
		return
	}
	for _, p := range s.state.Profiles {
		if sshChannelAddress(p.Peer.Address, p.Legacy) == address || sshChannelAddress(p.View.Address, p.Legacy) == address || sshChannelAddress(p.SavedAddress, false) == address {
			return
		}
	}
	s.ssh.ReleaseUnused([]string{address})
}
func (s *Service) holdSSHLocked(address string) func() {
	address = sshChannelAddress(address, false)
	if address == "" {
		return func() {}
	}
	if s.sshUses == nil {
		s.sshUses = map[string]int{}
	}
	s.sshUses[address]++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.sshUses[address]--
		if s.sshUses[address] == 0 {
			delete(s.sshUses, address)
		}
		s.releaseSSHIfUnusedLocked(address)
	}
}
