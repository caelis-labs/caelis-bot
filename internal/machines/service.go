// Package machines owns Bot peer profiles and frozen Worker routes. Local
// execution remains in the existing WorkRuntime, never in this transport.
package machines

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/sshtransport"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type profile struct {
	Label        string                `json:"label,omitempty"`
	LabelPending bool                  `json:"labelPending,omitempty"`
	PairingEpoch string                `json:"pairingEpoch"`
	PairingName  string                `json:"pairingName"`
	Peer         workerpeer.Connection `json:"peer"`
	View         api.Machine           `json:"view"`
}
type disk struct {
	Version         int                `json:"version"`
	ProfileID       string             `json:"profileId"`
	Profiles        map[string]profile `json:"profiles"`
	Routes          map[string]string  `json:"routes"`
	Runtimes        map[string]string  `json:"runtimes"`
	Reservations    map[string]bool    `json:"reservations,omitempty"`
	ReservationKeys map[string]string  `json:"reservationKeys,omitempty"`
}
type Service struct {
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

func Open(root string, local api.WorkRuntime) (*Service, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("machine store requires absolute path")
	}
	s := &Service{root: root, local: local, cache: map[string][]api.WorkState{}, cacheRevision: map[string]uint64{}, state: disk{Version: 2, ProfileID: rand.Text(), Profiles: map[string]profile{}, Routes: map[string]string{}, Runtimes: map[string]string{}, Reservations: map[string]bool{}, ReservationKeys: map[string]string{}}}
	s.ssh = sshtransport.New(filepath.Join(root, "SSH"))
	data, err := os.ReadFile(filepath.Join(root, "machines.json"))
	if err == nil {
		if json.Unmarshal(data, &s.state) != nil || s.state.Version != 2 || s.state.ProfileID == "" || s.state.Profiles == nil || s.state.Routes == nil || s.state.Runtimes == nil {
			s.loadErr = errors.New("machine store invalid")
		}
	} else if !os.IsNotExist(err) {
		s.loadErr = err
	}
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
		s.initializeLabelsLocked()
	}
	return s, s.loadErr
}
func (s *Service) save() error {
	if s.loadErr != nil {
		return s.loadErr
	}
	return localstate.Write(filepath.Join(s.root, "machines.json"), s.state)
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
	if s.loadErr == nil {
		return
	}
	b, err := os.ReadFile(filepath.Join(s.root, "machines.json"))
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
	return s.inspectBot(ctx, id, false, true)
}
func (s *Service) RemoveMachine(ctx context.Context, id string) (result error) {
	s.mu.Lock()
	p, ok := s.state.Profiles[id]
	defer func() {
		if result == nil {
			if target, ok := sshtransport.Target(p.Peer.Address); ok {
				_ = s.ssh.Stop(target)
			}
		}
	}()
	if !ok {
		s.mu.Unlock()
		return errors.New("machine_unknown")
	}
	// Existing new-protocol tasks retain their original routes. Removing a
	// presentation must never cancel work or discard the only control proof.
	if p.View.PairingStatus == "pending" || p.View.PairingStatus == "unknown" {
		old := p
		p.View.Archived = true
		s.state.Profiles[id] = p
		err := s.save()
		if err != nil {
			s.state.Profiles[id] = old
		}
		s.mu.Unlock()
		return err
	}
	for _, machine := range s.state.Routes {
		if machine == id {
			old := p
			p.View.Archived = true
			s.state.Profiles[id] = p
			err := s.save()
			if err != nil {
				s.state.Profiles[id] = old
			}
			s.mu.Unlock()
			return err
		}
	}
	s.mu.Unlock()
	// An unapproved request grants no execution. Forgetting it is local only;
	// B can reject/expire its inbox item. An approved source is unpaired first.
	if p.View.PairingStatus == "approved" {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		_, err := s.peerCall(c, p.Peer, workerpeer.Request{Action: "unpair"})
		cancel()
		if err != nil {
			return errors.New("peer_unpair_unconfirmed")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.state.Profiles[id]
	if !ok {
		return nil
	}
	if !sameBotConfiguration(current, p) {
		return errors.New("machine_connection_changed")
	}
	before := s.state.Profiles
	s.state.Profiles = maps.Clone(before)
	delete(s.state.Profiles, id)
	if err := s.save(); err != nil {
		s.state.Profiles = before
		return err
	}
	return nil
}
