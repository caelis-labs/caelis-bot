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
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type profile struct {
	View     api.Machine `json:"view"`
	Key      string      `json:"key"`
	Root     string      `json:"root"`
	Helper   string      `json:"helper"`
	Identity string      `json:"identity"`
}
type disk struct {
	Version   int                `json:"version"`
	ProfileID string             `json:"profileId"`
	Profiles  map[string]profile `json:"profiles"`
	Routes    map[string]string  `json:"routes"`
}
type Service struct {
	mu       sync.Mutex
	cacheMu  sync.RWMutex
	root     string
	state    disk
	secrets  map[string]string
	cache    map[string][]api.WorkState
	local    api.WorkRuntime
	artifact func(string) ([]byte, error)
}

func Open(root string, local api.WorkRuntime, artifact func(string) ([]byte, error)) (*Service, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("machine store requires absolute path")
	}
	s := &Service{root: root, local: local, artifact: artifact, secrets: map[string]string{}, cache: map[string][]api.WorkState{}, state: disk{Version: 1, ProfileID: rand.Text(), Profiles: map[string]profile{}, Routes: map[string]string{}}}
	b, e := os.ReadFile(filepath.Join(root, "machines.json"))
	if e == nil {
		if json.Unmarshal(b, &s.state) != nil || s.state.Version != 1 || s.state.Profiles == nil || s.state.Routes == nil {
			return nil, errors.New("machine store invalid")
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *Service) save() error {
	return localstate.Write(filepath.Join(s.root, "machines.json"), s.state)
}
func (s *Service) Machines() []api.Machine {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []api.Machine{}
	for _, p := range s.state.Profiles {
		out = append(out, p.View)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (s *Service) ConnectMachine(ctx context.Context, in api.MachineInput) (api.Machine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, jump, e := resolve(ctx, in)
	if e != nil {
		return api.Machine{ID: in.ID, Address: in.Address, State: "offline", Issue: e.Error()}, nil
	}
	key, fp, e := scanKey(ctx, v, jump)
	if e != nil {
		return api.Machine{ID: in.ID, Address: in.Address, Issue: e.Error(), State: "offline"}, nil
	}
	id := in.ID
	if id == "" {
		id = "machine-" + rand.Text()
	}
	old, exists := s.state.Profiles[id]
	if exists && (old.View.Address != in.Address || old.View.Port != v.Port || old.View.User != v.User) {
		for _, route := range s.state.Routes {
			if route == id {
				return old.View, errors.New("machine_has_tasks")
			}
		}
	}
	out := api.Machine{ID: id, Name: strings.TrimSpace(in.Name), Address: in.Address, Port: v.Port, User: v.User, Authentication: v.Authentication, SSHConfig: v.SSHConfig, PrivateKey: v.PrivateKey, Remember: v.Remember, Fingerprint: fp, State: "trust", Available: []string{}}
	if out.Name == "" {
		out.Name = in.Address
	}
	if len(out.Name) > 80 {
		return out, errors.New("invalid_name")
	}
	if exists && old.Key != "" && old.Key != key {
		out.Issue = "host_key_changed"
		return out, nil
	}
	if (!exists || old.Key != key) && in.TrustFingerprint != fp {
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
	if e = os.WriteFile(filepath.Join(d, "known_hosts"), []byte(host+" "+key+"\n"), 0600); e != nil {
		return out, e
	}
	p := old
	p.Key = key
	p.View = out
	p.View.Runtime = old.View.Runtime
	if in.Secret != "" {
		s.secrets[id] = in.Secret
	}
	if in.Secret != "" && in.Remember {
		if e = saveSecret(id, in.Secret); e != nil {
			return out, e
		}
	} else if !in.Remember {
		if e = deleteSecret(id); e != nil {
			return out, e
		}
	}
	// Resolve the target HOME and architecture through the authenticated channel.
	b, e := s.command(ctx, p, "printf '%s\\n' \"$HOME\"; uname -sm", nil)
	if e != nil {
		out.State = "offline"
		out.Issue = e.Error()
		p.View = out
		s.state.Profiles[id] = p
		_ = s.save()
		return out, nil
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "/") {
		return out, errors.New("target_environment")
	}
	arch := ""
	if lines[1] == "Linux x86_64" {
		arch = "amd64"
	} else if lines[1] == "Linux aarch64" {
		arch = "arm64"
	} else {
		return out, errors.New("unsupported_platform")
	}
	artifact, e := s.artifact(arch)
	if e != nil {
		return out, e
	}
	sum := sha256.Sum256(artifact)
	digest := hex.EncodeToString(sum[:])
	p.Root = filepath.Join(lines[0], ".local/share/caelis-bot", s.state.ProfileID, id)
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
	s.state.Profiles[id] = p
	if e = s.save(); e != nil {
		return out, e
	}
	return s.inspectLocked(ctx, id, p.View.Runtime)
}
func fmtPort(v int) string { return strings.TrimSpace(strconv.Itoa(v)) }
func (s *Service) call(ctx context.Context, p profile, r remotework.Request) (remotework.Response, error) {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inspectLocked(ctx, id, runtime)
}
func (s *Service) inspectLocked(ctx context.Context, id, runtime string) (api.Machine, error) {
	p, ok := s.state.Profiles[id]
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
	if p.View.Runtime != "" && p.View.Runtime != runtime {
		for _, route := range s.state.Routes {
			if route == id {
				return p.View, errors.New("machine_has_tasks")
			}
		}
	}
	r, e := s.call(ctx, p, remotework.Request{Action: "inspect", Runtime: runtime})
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
	s.state.Profiles[id] = p
	return p.View, s.save()
}
func (s *Service) SaveMachineModel(ctx context.Context, v api.MachineModel) (api.Machine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[v.ID]
	if !ok {
		return api.Machine{}, errors.New("machine_unknown")
	}
	_, e := s.call(ctx, p, remotework.Request{Action: "model", Model: v.Selection})
	if e != nil {
		return p.View, e
	}
	return s.inspectLocked(ctx, v.ID, p.View.Runtime)
}
func (s *Service) ChangeMachineTeam(ctx context.Context, v api.MachineTeamChange) (api.RuntimeMutationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[v.ID]
	if !ok {
		return api.RuntimeMutationResult{}, errors.New("machine_unknown")
	}
	r, e := s.call(ctx, p, remotework.Request{Action: "team", Team: v.Change})
	return r.Mutation, e
}
func (s *Service) ReadMachineAdvanced(ctx context.Context, id string) (api.Machine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[id]
	if !ok {
		return api.Machine{}, errors.New("machine_unknown")
	}
	r, e := s.call(ctx, p, remotework.Request{Action: "configuration"})
	p.View.Configuration = r.Configuration
	p.View.AdvancedIssue = r.AdvancedIssue
	if e != nil {
		p.View.AdvancedIssue = e.Error()
	}
	s.state.Profiles[id] = p
	return p.View, nil
}
func (s *Service) MachineTerminal(ctx context.Context, id string) (api.TerminalTarget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[id]
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
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, route := range s.state.Routes {
		if route == id {
			return errors.New("machine_has_tasks")
		}
	}
	if err := deleteSecret(id); err != nil {
		return err
	}
	delete(s.state.Profiles, id)
	delete(s.secrets, id)
	return s.save()
}
