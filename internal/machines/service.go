// Package machines owns user SSH profiles, target identity, and typed worker
// routes. It never imports a remote path into the local filesystem.
package machines

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/remotework"
	"github.com/caelis-labs/caelis-bot/internal/secretstore"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type profile struct {
	PairingCodeDigest  string                `json:"pairingCodeDigest,omitempty"`
	RetainPairingProof bool                  `json:"retainPairingProof,omitempty"`
	PairingEpoch       string                `json:"pairingEpoch,omitempty"`
	Peer               workerpeer.Connection `json:"peer,omitempty"`
	View               api.Machine           `json:"view"`
	Key                string                `json:"key"`
	Root               string                `json:"root"`
	Helper             string                `json:"helper"`
	Identity           string                `json:"identity"`
}
type disk struct {
	Version   int                `json:"version"`
	ProfileID string             `json:"profileId"`
	Profiles  map[string]profile `json:"profiles"`
	Routes    map[string]string  `json:"routes"`
	Runtimes  map[string]string  `json:"runtimes,omitempty"`
	// Reservations have never dispatched a Worker. Legacy routes are owners.
	Reservations    map[string]bool   `json:"reservations,omitempty"`
	ReservationKeys map[string]string `json:"reservationKeys,omitempty"`
}
type Service struct {
	connectionMu            sync.Mutex
	connections             map[string]*connectionGate
	pairingActive           map[string]int // mu; live invocations only, never a receipt ledger
	peerApprovalFingerprint string         // cacheMu; presentation change evidence only
	peerApprovalRevision    uint64         // cacheMu; monotonic, never native execution state
	loadErr                 error
	request                 func(context.Context, profile, remotework.Request) (remotework.Response, error)
	mu                      sync.Mutex
	secretMu                sync.RWMutex
	secretStore             secretstore.Store
	cacheMu                 sync.RWMutex
	root                    string
	state                   disk
	secrets                 map[string]string
	cache                   map[string][]api.WorkState
	cacheRevision           map[string]uint64              // guarded by mu; fences late observations
	observations            map[string]api.WorkObservation // guarded by cacheMu
	observationAt           map[string]time.Time           // successful current snapshots; not execution state
	local                   api.WorkRuntime
	artifact                func(string) ([]byte, error)
}

func Open(root string, local api.WorkRuntime, artifact func(string) ([]byte, error)) (*Service, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("machine store requires absolute path")
	}
	s := &Service{root: root, local: local, artifact: artifact, secrets: map[string]string{},
		secretStore: secretstore.Functions{SaveFunc: saveSecret, LoadFunc: loadSecret, DeleteFunc: deleteSecret},
		cache:       map[string][]api.WorkState{}, cacheRevision: map[string]uint64{}, state: disk{Version: 1, ProfileID: rand.Text(), Profiles: map[string]profile{}, Routes: map[string]string{}}}
	b, e := os.ReadFile(filepath.Join(root, "machines.json"))
	if e == nil {
		if json.Unmarshal(b, &s.state) != nil || s.state.Version != 1 || s.state.Profiles == nil || s.state.Routes == nil {
			s.loadErr = errors.New("machine store invalid")
			s.state = disk{Version: 1, Profiles: map[string]profile{}, Routes: map[string]string{}}
		}
	} else if !os.IsNotExist(e) {
		s.loadErr = e
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		s.loadErr = e
	}
	if s.state.Runtimes == nil {
		s.state.Runtimes = map[string]string{}
	}
	if s.state.Reservations == nil {
		s.state.Reservations = map[string]bool{}
	}
	// v1 profiles could not change runtime while owning tasks, so their retained
	// runtime is the authoritative migration source. Persist before any switch.
	changed := false
	for id, machine := range s.state.Routes {
		if s.state.Runtimes[id] == "" {
			runtime := s.state.Profiles[machine].View.Runtime
			if runtime != "codex" && runtime != "caelis" {
				s.loadErr = errors.New("original_task_runtime_unavailable")
				continue
			}
			s.state.Runtimes[id], changed = runtime, true
		}
		if runtime := s.state.Runtimes[id]; runtime != "codex" && runtime != "caelis" {
			s.loadErr = errors.New("original_task_runtime_unavailable")
			continue
		}
	}
	if changed && s.loadErr == nil {
		if e = s.save(); e != nil {
			s.loadErr = e
		}
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
	if err != nil || json.Unmarshal(b, &restored) != nil || restored.Version != 1 || restored.ProfileID == "" || restored.Profiles == nil || restored.Routes == nil {
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
}
func (s *Service) ConnectMachine(ctx context.Context, in api.MachineInput) (api.Machine, error) {
	if len(strings.TrimSpace(in.Name)) > 80 {
		return api.Machine{}, &api.ActionRejectedError{Code: "invalid_name"}
	}
	id := in.ID
	if id == "" {
		id = "machine-" + rand.Text()
	}
	release, e := s.lockSSHConnection(ctx, id)
	if e != nil {
		return api.Machine{}, e
	}
	defer release()
	v, jump, e := resolve(ctx, in)
	if e != nil {
		return api.Machine{ID: in.ID, Address: in.Address, State: "offline", Issue: e.Error()}, nil
	}
	s.mu.Lock()
	old, exists := s.state.Profiles[id]
	profileID := s.state.ProfileID
	if exists && old.View.ConnectionType == "bot" {
		s.mu.Unlock()
		return old.View, &api.ActionRejectedError{Code: "machine_connection_type_conflict"}
	}
	if exists && (old.View.Address != in.Address || old.View.Port != v.Port || old.View.User != v.User) {
		for task, route := range s.state.Routes {
			if route == id && !s.state.Reservations[task] {
				s.mu.Unlock()
				return old.View, &api.ActionRejectedError{Code: "machine_has_tasks"}
			}
		}
	}
	s.mu.Unlock()
	key, fp := old.Key, old.View.Fingerprint
	if !in.SSHConfig {
		key, fp, e = scanKey(ctx, v, jump)
		if e != nil {
			return api.Machine{ID: id, Address: in.Address, Issue: e.Error(), State: "offline"}, nil
		}
	}
	out := api.Machine{ConnectionType: "ssh", ID: id, Name: strings.TrimSpace(in.Name), Address: in.Address, Port: v.Port, User: v.User, Authentication: v.Authentication, SSHConfig: v.SSHConfig, PrivateKey: v.PrivateKey, Remember: v.Remember, Fingerprint: fp, State: "trust", Available: []string{}}
	if out.Name == "" {
		out.Name = in.Address
	}
	if !in.SSHConfig && exists && old.Key != "" && old.Key != key {
		out.Issue = "host_key_changed"
		return out, nil
	}
	if !in.SSHConfig && (!exists || old.Key != key) && in.TrustFingerprint != fp {
		out.Issue = "confirm_fingerprint"
		return out, nil
	}
	d := filepath.Join(s.root, id)
	if e = os.MkdirAll(d, 0700); e != nil {
		return out, e
	}
	host := v.Address
	if v.Port != 22 {
		host = "[" + host + "]:" + fmtPort(v.Port)
	}
	// Preserve the prior public pin until the read-only platform probe admits
	// this connection. The per-profile gate protects restoration from editors.
	priorPin, pinErr := os.ReadFile(filepath.Join(d, "known_hosts"))
	if pinErr != nil && !os.IsNotExist(pinErr) {
		return out, pinErr
	}
	if key != "" {
		if e = os.WriteFile(filepath.Join(d, "known_hosts"), []byte(host+" "+key+"\n"), 0600); e != nil {
			return out, e
		}
	}
	p := old
	p.Key = key
	p.View = out
	p.View.Runtime = old.View.Runtime
	s.secretMu.Lock()
	priorSecret, hadSecret := s.secrets[id]
	if in.Secret != "" {
		s.secrets[id] = in.Secret
	}
	s.secretMu.Unlock()
	rejectProbe := func(code string) (api.Machine, error) {
		s.secretMu.Lock()
		if hadSecret {
			s.secrets[id] = priorSecret
		} else {
			delete(s.secrets, id)
		}
		s.secretMu.Unlock()
		if key != "" {
			var err error
			if os.IsNotExist(pinErr) {
				err = os.Remove(filepath.Join(d, "known_hosts"))
			} else {
				err = os.WriteFile(filepath.Join(d, "known_hosts"), priorPin, 0600)
			}
			if err != nil {
				return out, err
			} // Failed restoration is not a certified refusal.
		}
		return out, &api.ActionRejectedError{Code: code}
	}
	persistCredential := func() error {
		if in.Secret != "" && in.Remember {
			return s.secretStore.Save(id, in.Secret)
		}
		if !in.Remember {
			return s.secretStore.Delete(id)
		}
		return nil
	}
	// Resolve the target HOME and architecture through the authenticated channel.
	b, e := s.command(ctx, p, "printf '%s\\n' \"$HOME\"; uname -sm", nil)
	if e != nil {
		out.State = "offline"
		out.Issue = e.Error()
		p.View = out
		if e = persistCredential(); e != nil {
			return out, e
		}
		if _, saveErr := s.commitSSHProfile(id, old, exists, p); saveErr != nil {
			return out, saveErr
		}
		return out, nil
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "/") {
		return rejectProbe("target_environment")
	}
	arch := ""
	if lines[1] == "Linux x86_64" {
		arch = "amd64"
	} else if lines[1] == "Linux aarch64" {
		arch = "arm64"
	} else {
		return rejectProbe("unsupported_platform")
	}
	if e = persistCredential(); e != nil {
		return out, e
	}
	artifact, e := s.artifact(arch)
	if e != nil {
		return out, e
	}
	sum := sha256.Sum256(artifact)
	digest := hex.EncodeToString(sum[:])
	p.Root = filepath.Join(lines[0], ".local/share/caelis-bot", profileID, id)
	p.Helper = filepath.Join(p.Root, "helper-"+digest)
	// A reviewed app-built helper only, never an installation of Codex/Caelis.
	setup := "umask 077; mkdir -p " + quote(p.Root) + " && test ! -L " + quote(p.Root) + " && test ! -L " + quote(p.Helper) + " && test ! -L " + quote(p.Helper+".upload") + " && cat > " + quote(p.Helper+".upload") + " && test \"$(sha256sum " + quote(p.Helper+".upload") + " | cut -d ' ' -f 1)\" = " + quote(digest) + " && chmod 700 " + quote(p.Helper+".upload") + " && mv " + quote(p.Helper+".upload") + " " + quote(p.Helper)
	if _, e = s.command(ctx, p, setup, strings.NewReader(string(artifact))); e != nil {
		return out, e
	}
	r, e := s.call(ctx, p, remotework.Request{Action: "detect"})
	if e != nil {
		return out, e
	}
	if old.Identity != "" && old.Identity != r.Identity {
		return old.View, errors.New("target_identity_changed")
	}
	p.Identity = r.Identity
	p.View.Available = r.Available
	p.View.State = "setup"
	p.View.Issue = ""
	if _, e = s.commitSSHProfile(id, old, exists, p); e != nil {
		return out, e
	}
	return s.inspectSSH(ctx, id, p.View.Runtime, true)
}
func fmtPort(v int) string { return strings.TrimSpace(strconv.Itoa(v)) }
func (s *Service) call(ctx context.Context, p profile, r remotework.Request) (remotework.Response, error) {
	if p.View.ConnectionType == "bot" {
		return s.peerCall(ctx, p, r)
	}
	if s.request != nil {
		return s.request(ctx, p, r)
	}
	r.Version = remotework.Version
	r.Runtime = first(r.Runtime, p.View.Runtime)
	b, _ := json.Marshal(r)
	out, e := s.command(ctx, p, quote(p.Helper)+" proxy "+quote(p.Root), strings.NewReader(string(b)+"\n"))
	if e != nil {
		return remotework.Response{}, e
	}
	var v remotework.Response
	if json.Unmarshal(out, &v) != nil || v.Version != remotework.Version || v.Identity == "" {
		return v, errors.New("helper_incompatible")
	}
	if p.Identity != "" && p.Identity != v.Identity {
		return v, errors.New("target_identity_changed")
	}
	if v.Error != "" {
		return v, errors.New(v.Error)
	}
	return v, nil
}
func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func (s *Service) InspectMachine(ctx context.Context, id, runtime string) (api.Machine, error) {
	p, ok := s.machineProfile(id)
	if !ok {
		return api.Machine{}, errors.New("machine_unknown")
	}
	if p.View.ConnectionType == "bot" {
		return s.inspectBot(ctx, id, false, true)
	}
	if runtime != "" && runtime != p.View.Runtime {
		release, e := s.lockSSHConnection(ctx, id)
		if e != nil {
			return p.View, e
		}
		defer release()
		current, exists := s.machineProfile(id)
		if !exists || !sameSSHConfiguration(current, p) {
			return current.View, errors.New("machine_connection_changed")
		}
	}
	return s.inspectSSH(ctx, id, runtime, true)
}
func (s *Service) machineProfile(id string) (profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[id]
	return p, ok
}
func sameConnectionConfiguration(a, b profile) bool {
	if a.View.ConnectionType == "bot" || b.View.ConnectionType == "bot" {
		return sameBotConfiguration(a, b)
	}
	return sameSSHConfiguration(a, b)
}

func sameSSHConfiguration(a, b profile) bool {
	x, y := a.View, b.View
	return a.Key == b.Key && a.Root == b.Root && a.Helper == b.Helper && a.Identity == b.Identity && x.ID == y.ID && x.Address == y.Address && x.Port == y.Port && x.User == y.User && x.Name == y.Name && x.SSHConfig == y.SSHConfig && x.Authentication == y.Authentication && x.PrivateKey == y.PrivateKey && x.Remember == y.Remember && x.Runtime == y.Runtime
}
func (s *Service) commitSSHProfile(id string, old profile, existed bool, next profile) (api.Machine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.state.Profiles[id]
	if exists != existed || exists && !sameSSHConfiguration(current, old) {
		return current.View, errors.New("machine_connection_changed")
	}
	s.state.Profiles[id] = next
	if e := s.save(); e != nil {
		if existed {
			s.state.Profiles[id] = current
		} else {
			delete(s.state.Profiles, id)
		}
		return current.View, e
	}
	return next.View, nil
}
func (s *Service) inspectSSH(ctx context.Context, id, runtime string, renew bool) (api.Machine, error) {
	p, ok := s.machineProfile(id)
	if !ok {
		return api.Machine{}, errors.New("machine_unknown")
	}
	if runtime == "" && len(p.View.Available) == 1 {
		runtime = p.View.Available[0]
	}
	if runtime == "" {
		return p.View, nil
	}
	if runtime != "codex" && runtime != "caelis" {
		return p.View, errors.New("unsupported_runtime")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	r, e := s.call(ctx, p, remotework.Request{Action: "inspect", Runtime: runtime})
	next := inspectedProfile(p, runtime, r, e)
	view, err := s.commitSSHProfile(id, p, true, next)
	if err == nil && renew && view.State == "ready" {
		s.cacheMu.Lock()
		s.cacheRevision[id+"\x00"+runtime]++
		s.cacheMu.Unlock()
	}
	return view, err
}

func inspectedProfile(p profile, runtime string, r remotework.Response, e error) profile {
	p.View.CheckedAt = time.Now().UnixMilli()
	p.View.Runtime = runtime
	p.View.Models = []api.ModelOption{}
	p.View.RuntimeDefault = nil
	if e != nil {
		p.View.State = "offline"
		p.View.Issue = e.Error()
	} else {
		p.View.Setup = r.Setup
		p.View.Work = r.Model
		p.View.Models = r.Models
		p.View.RuntimeDefault = r.RuntimeDefault
		p.View.Available = r.Available
		p.View.State = "setup"
		p.View.Issue = ""
		if r.Setup.State == "ready" {
			p.View.State = "ready"
		}
	}
	return p
}
func (s *Service) SaveMachineModel(ctx context.Context, v api.MachineModel) (api.Machine, error) {
	p, ok := s.machineProfile(v.ID)
	if !ok {
		return api.Machine{}, errors.New("machine_unknown")
	}
	_, e := s.call(ctx, p, remotework.Request{Action: "model", Model: v.Selection})
	if e != nil {
		return p.View, e
	}
	return s.inspectSSH(ctx, v.ID, p.View.Runtime, true)
}
func (s *Service) ChangeMachineTeam(ctx context.Context, v api.MachineTeamChange) (api.RuntimeMutationResult, error) {
	p, ok := s.machineProfile(v.ID)
	if !ok {
		return api.RuntimeMutationResult{}, errors.New("machine_unknown")
	}
	r, e := s.call(ctx, p, remotework.Request{Action: "team", Team: v.Change})
	return r.Mutation, e
}
func (s *Service) ReadMachineAdvanced(ctx context.Context, id string) (api.Machine, error) {
	p, ok := s.machineProfile(id)
	if !ok {
		return api.Machine{}, errors.New("machine_unknown")
	}
	r, e := s.call(ctx, p, remotework.Request{Action: "configuration"})
	p.View.Configuration = r.Configuration
	p.View.AdvancedIssue = r.AdvancedIssue
	if e != nil {
		p.View.AdvancedIssue = e.Error()
	}
	// Configuration reads are display-only; do not overwrite a newer route.
	current, exists := s.machineProfile(id)
	if !exists || !sameSSHConfiguration(current, p) {
		return current.View, errors.New("machine_connection_changed")
	}
	return p.View, nil
}
func (s *Service) MachineTerminal(ctx context.Context, id string) (api.TerminalTarget, error) {
	p, ok := s.machineProfile(id)
	if !ok {
		return api.TerminalTarget{}, errors.New("machine_unknown")
	}
	r, e := s.call(ctx, p, remotework.Request{Action: "tui"})
	if e != nil {
		return api.TerminalTarget{}, e
	}
	r.Terminal.SSH, e = s.sshArgs(p, true)
	return r.Terminal, e
}
func (s *Service) RemoveMachine(ctx context.Context, id string) error {
	before, existed := s.machineProfile(id)
	if existed && before.View.ConnectionType != "bot" {
		release, e := s.lockSSHConnection(ctx, id)
		if e != nil {
			return e
		}
		defer release()
		current, exists := s.machineProfile(id)
		if !exists || !sameSSHConfiguration(current, before) {
			return errors.New("machine_connection_changed")
		}
	}

	s.mu.Lock()
	p, exists := s.state.Profiles[id]
	if !exists {
		s.mu.Unlock()
		return errors.New("machine_unknown")
	}
	hasTasks := false
	for task, route := range s.state.Routes {
		if route == id && !s.state.Reservations[task] {
			hasTasks = true
		}
	}
	if hasTasks || p.View.ConnectionType == "bot" && p.RetainPairingProof {
		if p.View.ConnectionType != "bot" {
			s.mu.Unlock()
			return errors.New("machine_has_tasks")
		}
		// Product removal hides this target from new delegation, not its task
		// ownership. Keep the only original proof and every retained route.
		next := p
		next.View.Archived = true
		s.state.Profiles[id] = next
		err := s.save()
		if err != nil {
			s.state.Profiles[id] = p
		}
		s.mu.Unlock()
		return err
	}
	if p.View.ConnectionType == "bot" {
		s.mu.Unlock()
		checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		_, err := workerpeer.Call(checkCtx, p.Peer, workerpeer.Request{Action: "unpair"})
		cancel()
		if err != nil {
			return errors.Join(errors.New("peer_unpair_unconfirmed"), err)
		}
		s.mu.Lock()
		current, ok := s.state.Profiles[id]
		if !ok || !sameBotConfiguration(current, p) {
			s.mu.Unlock()
			return errors.New("peer_unpair_unconfirmed")
		}
		for task, route := range s.state.Routes {
			if route == id && !s.state.Reservations[task] {
				s.mu.Unlock()
				return errors.New("peer_unpair_unconfirmed")
			}
		}
	} else if err := s.secretStore.Delete(id); err != nil {
		s.mu.Unlock()
		return err
	}
	defer s.mu.Unlock()
	old := s.state
	s.state.Profiles = maps.Clone(old.Profiles)
	s.state.Routes = maps.Clone(old.Routes)
	s.state.Runtimes = maps.Clone(old.Runtimes)
	s.state.Reservations = maps.Clone(old.Reservations)
	s.state.ReservationKeys = maps.Clone(old.ReservationKeys)
	delete(s.state.Profiles, id)
	// Confirmed unpair releases only undispatched preparation bindings.
	for task, route := range s.state.Routes {
		if route == id {
			delete(s.state.Routes, task)
			delete(s.state.Runtimes, task)
			delete(s.state.Reservations, task)
			delete(s.state.ReservationKeys, task)
		}
	}
	if err := s.save(); err != nil {
		s.state = old
		return err
	}
	s.secretMu.Lock()
	delete(s.secrets, id)
	s.secretMu.Unlock()
	return nil
}
