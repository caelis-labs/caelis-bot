// Package machines owns Bot peer profiles and frozen Worker routes. Local
// execution remains in the existing WorkRuntime, never in this transport.
package machines

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/sshtransport"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type profile struct {
	Source       string                `json:"source,omitempty"`
	SavedAddress string                `json:"savedAddress,omitempty"`
	Legacy       bool                  `json:"legacy,omitempty"`
	Label        string                `json:"label,omitempty"`
	LabelPending bool                  `json:"labelPending,omitempty"`
	PairingEpoch string                `json:"pairingEpoch"`
	PairingName  string                `json:"pairingName"`
	Peer         workerpeer.Connection `json:"peer"`
	View         api.Machine           `json:"view"`
}
type disk struct {
	MutationFloor   int64                                `json:"mutationFloor,omitempty"`
	Mutations       map[string]api.MachineMutationResult `json:"mutations,omitempty"`
	Version         int                                  `json:"version"`
	ProfileID       string                               `json:"profileId"`
	Profiles        map[string]profile                   `json:"profiles"`
	Routes          map[string]string                    `json:"routes"`
	Runtimes        map[string]string                    `json:"runtimes"`
	Reservations    map[string]bool                      `json:"reservations,omitempty"`
	ReservationKeys map[string]string                    `json:"reservationKeys,omitempty"`
}
type Service struct {
	diagnostics             func(diagnosticlog.Record)
	deleteLegacyCredential  func(string) error
	sshUses                 map[string]int
	registry                string
	legacyUnpublished       bool
	mutationMu              sync.Mutex
	mutations               map[string]api.MachineMutationResult
	ssh                     *sshtransport.Transport
	pairingActive           map[string]int
	peerApprovalFingerprint string
	peerApprovalRevision    uint64
	loadErr                 error
	mu                      sync.Mutex
	cacheMu                 sync.RWMutex
	root                    string
	state                   disk
	cache                   map[string][]api.WorkState
	cacheRevision           map[string]uint64
	observations            map[string]api.WorkObservation
	observationAt           map[string]time.Time
	local                   api.WorkRuntime
}

// Open never accesses desktop credentials. The app supplies its platform cleanup
// capability explicitly; isolated fixtures cannot delete real Keychain items.
func Open(root string, local api.WorkRuntime) (*Service, error) {
	return OpenWithCredentialCleanup(root, local, nil)
}
func OpenWithCredentialCleanup(root string, local api.WorkRuntime, cleanup func(string) error) (*Service, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("machine store requires absolute path")
	}
	s := &Service{deleteLegacyCredential: cleanup, sshUses: map[string]int{}, root: root, local: local, cache: map[string][]api.WorkState{}, cacheRevision: map[string]uint64{}, state: disk{Version: 2, ProfileID: rand.Text(), Profiles: map[string]profile{}, Routes: map[string]string{}, Runtimes: map[string]string{}, Reservations: map[string]bool{}, ReservationKeys: map[string]string{}}}
	s.ssh = sshtransport.New(filepath.Join(root, "SSH"))
	s.registry = filepath.Join(root, "machines.json")
	if _, err := os.Stat(filepath.Join(root, "machines-v2.json")); err == nil {
		s.registry = filepath.Join(root, "machines-v2.json")
	}
	data, err := os.ReadFile(s.registry)
	if err == nil {
		if json.Unmarshal(data, &s.state) != nil || (s.state.Version != 1 && s.state.Version != 2) || s.state.ProfileID == "" || s.state.Profiles == nil || s.state.Routes == nil || s.state.Runtimes == nil {
			s.loadErr = errors.New("machine store invalid")
		}
	} else if !os.IsNotExist(err) {
		s.loadErr = err
	}
	if s.loadErr == nil && s.state.Version == 1 {
		// Keep the complete original v1 document, including legacy credentials and
		// routes. The new registry never invents v2 authorization for these rows.
		s.registry = filepath.Join(root, "machines-v2.json")
		s.state = migratedLegacy(s.state)
		s.legacyUnpublished = true
	}
	if s.state.Mutations == nil {
		s.state.Mutations = map[string]api.MachineMutationResult{}
	}
	s.mutations = map[string]api.MachineMutationResult{}
	if s.state.Reservations == nil {
		s.state.Reservations = map[string]bool{}
	}
	if s.state.ReservationKeys == nil {
		s.state.ReservationKeys = map[string]string{}
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		s.loadErr = err
	}
	if s.loadErr == nil {
		// Older releases hid removed connections. Upgrade that explicit removal
		// intent locally; neither task history nor remote grants are rewritten.
		for id, p := range s.state.Profiles {
			if p.View.Archived {
				s.ManageMachine(api.MachineMutation{ID: newMachineMutationID(), MachineID: id, Action: "remove"})
			}
		}
		for id, r := range s.state.Mutations {
			if r.CleanupPending {
				s.MachineMutation(id)
			}
		}
	}
	if s.loadErr == nil {
		s.initializeLabelsLocked()
	}
	return s, s.loadErr
}
func (s *Service) save() error {
	if s.loadErr != nil {
		return s.loadErr
	}
	err := localstate.Write(s.registry, s.state)
	if err == nil {
		s.legacyUnpublished = false
	}
	return err
}
func (s *Service) Machines() []api.Machine {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadLocked()
	out := []api.Machine{}
	for _, p := range s.state.Profiles {
		out = append(out, machineView(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Adopt only the repaired original registry. Removing an unreadable registry
// cannot authorize replacing its owners/routes with a fresh profile.
func (s *Service) reloadLocked() {
	if s.loadErr == nil || s.loadErr.Error() == "machine_save_unknown" {
		return
	}
	b, err := os.ReadFile(s.registry)
	var restored disk
	if err != nil || json.Unmarshal(b, &restored) != nil || restored.Version != 2 || restored.ProfileID == "" || restored.Profiles == nil || restored.Routes == nil {
		return
	}
	if restored.Runtimes == nil {
		restored.Runtimes = map[string]string{}
	}
	for id, machine := range restored.Routes {
		if restored.Runtimes[id] == "" {
			restored.Runtimes[id] = restored.Profiles[machine].View.Runtime
		}
		if restored.Runtimes[id] != "codex" && restored.Runtimes[id] != "caelis" {
			return
		}
	}
	if restored.Mutations == nil {
		restored.Mutations = map[string]api.MachineMutationResult{}
	}
	if restored.Reservations == nil {
		restored.Reservations = map[string]bool{}
	}
	s.state, s.loadErr = restored, nil
	s.initializeLabelsLocked()
}

func (s *Service) machineProfile(id string) (profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[id]
	return p, ok
}
func sameConnectionConfiguration(a, b profile) bool { return sameBotConfiguration(a, b) }
func (s *Service) InspectMachine(ctx context.Context, id, runtime string) (api.Machine, error) {
	if p, ok := s.machineProfile(id); ok && p.Legacy {
		return machineView(p), nil
	}
	return s.inspectBot(ctx, id, false, true)
}
func (s *Service) RemoveMachine(ctx context.Context, id string) error {
	result := s.ManageMachine(api.MachineMutation{ID: newMachineMutationID(), MachineID: id, Action: "remove"})
	if result.Status != "succeeded" {
		return errors.New(result.Code)
	}
	return nil
}

func migratedLegacy(value disk) disk {
	value.Version = 2
	for id, p := range value.Profiles {
		p.Legacy = true
		p.Label = p.View.Name
		p.View.State, p.View.Issue, p.View.PairingStatus = "legacy", "machine_legacy", ""
		value.Profiles[id] = p
	}
	return value
}
