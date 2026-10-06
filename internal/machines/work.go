package machines

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/remotework"
	"reflect"
	"sync"
	"time"
)

func (s *Service) TaskMachines() []api.TaskMachine {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []api.TaskMachine{}
	for _, p := range s.state.Profiles {
		out = append(out, api.TaskMachine{ID: p.View.ID, Name: p.View.Name, Runtime: p.View.Runtime, Ready: p.View.State == "ready"})
	}
	return out
}
func (s *Service) WorkAdmission(ctx context.Context) error { return s.local.WorkAdmission(ctx) }
func (s *Service) WorkStates() []api.WorkState {
	out := s.local.WorkStates()
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	for _, states := range s.cache {
		out = append(out, states...)
	}
	return out
}
func (s *Service) cacheResponse(id, name, runtime string, states []api.WorkState) {
	owned := states[:0]
	for _, state := range states {
		if s.state.Routes[state.Task.ID] == id && s.state.Runtimes[state.Task.ID] == runtime {
			owned = append(owned, state)
		}
	}
	states = owned
	for i := range states {
		states[i].Runtime = runtime
		states[i].Task.Machine = id
		states[i].Task.MachineName = name
	}
	s.cacheMu.Lock()
	s.cacheRevision[id+"\x00"+runtime]++
	s.cache[id+"\x00"+runtime] = states
	s.cacheMu.Unlock()
}
func (s *Service) PrepareRemoteWork(ctx context.Context, in api.TaskStart, id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[in.Machine]
	if !ok {
		return "", errors.New("machine_not_ready")
	}
	// bindLocked checks readiness for a new route. A recorded preparation keeps
	// its original backend even if the new default is currently unconfigured.
	runtime, e := s.bindLocked(in, id, "")
	if e != nil {
		return "", e
	}
	r, e := s.call(ctx, p, remotework.Request{Action: "prepare", Runtime: runtime, ID: id, Start: api.WorkStart{TaskStart: in}})
	if e != nil {
		return "", errors.Join(e, s.releasePreparationLocked(id))
	}
	return r.Task.Workspace, nil
}
func (s *Service) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	if in.Machine == "" {
		return s.local.StartWork(ctx, in)
	}
	if e := s.local.WorkAdmission(ctx); e != nil {
		return api.Task{}, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	runtime, e := s.bindLocked(in.TaskStart, in.ID, "")
	if e != nil {
		return api.Task{}, e
	}
	// Crossing this fence makes the route an owner even if dispatch's outcome is
	// unknown. Preparation cleanup must never release it after this point.
	if s.state.Reservations[in.ID] {
		delete(s.state.Reservations, in.ID)
		if e = s.save(); e != nil {
			s.state.Reservations[in.ID] = true
			return api.Task{}, e
		}
	}
	p := s.state.Profiles[in.Machine]
	r, e := s.call(ctx, p, remotework.Request{Action: "start", Runtime: runtime, Start: in})
	r.Task.Machine = in.Machine
	r.Task.MachineName = p.View.Name
	s.refreshCacheLocked(ctx, in.Machine, p, runtime)
	return r.Task, e
}

type workPoll struct {
	id, runtime string
	profile     profile
	revision    uint64
}

func (s *Service) workPolls() []workPoll {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	out := []workPoll{}
	for task, id := range s.state.Routes {
		if s.state.Reservations[task] {
			continue
		}
		runtime := s.state.Runtimes[task]
		key := id + "\x00" + runtime
		if p, ok := s.state.Profiles[id]; ok && !seen[key] {
			seen[key] = true
			out = append(out, workPoll{id, runtime, p, s.cacheRevision[key]})
		}
	}
	return out
}

// Network observation must never own the global route/configuration lock.
// Apply only to the unchanged profile and still-owned backend; a late response
// cannot restore a deleted machine or overwrite a newly selected default.
func (s *Service) pollWork(ctx context.Context, poll workPoll) {
	r, err := s.call(ctx, poll.profile, remotework.Request{Action: "states", Runtime: poll.runtime})
	var inspected remotework.Response
	var inspectErr error
	recheck := err == nil && poll.profile.View.Runtime == poll.runtime && poll.profile.View.State == "offline"
	if recheck {
		inspected, inspectErr = s.call(ctx, poll.profile, remotework.Request{Action: "inspect", Runtime: poll.runtime})
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, exists := s.state.Profiles[poll.id]
	if !exists || !reflect.DeepEqual(p, poll.profile) || s.cacheRevision[poll.id+"\x00"+poll.runtime] != poll.revision {
		return
	}
	owned := false
	for task, machine := range s.state.Routes {
		if machine == poll.id && s.state.Runtimes[task] == poll.runtime && !s.state.Reservations[task] {
			owned = true
			break
		}
	}
	if !owned {
		return
	}
	if err == nil {
		s.cacheResponse(poll.id, p.View.Name, poll.runtime, r.States)
		if recheck {
			next := inspectedProfile(p, poll.runtime, inspected, inspectErr)
			s.state.Profiles[poll.id] = next
			if s.save() != nil {
				s.state.Profiles[poll.id] = p
			}
		}
		return
	}
	if p.View.Runtime == poll.runtime && (p.View.State != "offline" || p.View.Issue != err.Error()) {
		next := p
		next.View.State, next.View.Issue = "offline", err.Error()
		s.state.Profiles[poll.id] = next
		if s.save() != nil {
			s.state.Profiles[poll.id] = p
		}
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.cacheRevision[poll.id+"\x00"+poll.runtime]++
	for i := range s.cache[poll.id+"\x00"+poll.runtime] {
		state := &s.cache[poll.id+"\x00"+poll.runtime][i]
		if status := state.Task.Status; status != "completed" && status != "failed" && status != "interrupted" && status != "cancelled" {
			state.Task.Status = "unknown"
		}
	}
}
func (s *Service) remote(ctx context.Context, id, action string, message api.TaskMessage) (api.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	machine := s.state.Routes[id]
	p, ok := s.state.Profiles[machine]
	if !ok {
		return api.Task{}, errors.New("original_machine_unavailable")
	}
	r, e := s.call(ctx, p, remotework.Request{Action: action, Runtime: s.state.Runtimes[id], ID: id, Message: message})
	r.Task.Machine = machine
	r.Task.MachineName = p.View.Name
	s.refreshCacheLocked(ctx, machine, p, s.state.Runtimes[id])
	return r.Task, e
}
func (s *Service) route(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Routes[id]
}
func (s *Service) ReadWork(ctx context.Context, id string) (api.Task, error) {
	if s.route(id) == "" {
		return s.local.ReadWork(ctx, id)
	}
	return s.remote(ctx, id, "read", api.TaskMessage{})
}
func (s *Service) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	if s.route(in.ID) == "" {
		return s.local.SendWork(ctx, in)
	}
	if e := s.local.WorkAdmission(ctx); e != nil {
		return api.Task{}, e
	}
	return s.remote(ctx, in.ID, "send", in)
}
func (s *Service) StopWork(ctx context.Context, id string) (api.Task, error) {
	if s.route(id) == "" {
		return s.local.StopWork(ctx, id)
	}
	return s.remote(ctx, id, "stop", api.TaskMessage{})
}
func (s *Service) WorkMessageRecorded(in api.TaskMessage) bool {
	if s.route(in.ID) != "" {
		return false
	}
	p, ok := s.local.(api.RecordedWorkMessage)
	return ok && p.WorkMessageRecorded(in)
}
func (s *Service) WorkTerminal(ctx context.Context, id string) (api.TerminalTarget, error) {
	if s.route(id) == "" {
		p, ok := s.local.(api.WorkTerminalProvider)
		if !ok {
			return api.TerminalTarget{}, errors.New("terminal unavailable")
		}
		return p.WorkTerminal(ctx, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[s.state.Routes[id]]
	if !ok {
		return api.TerminalTarget{}, errors.New("original_machine_unavailable")
	}
	r, e := s.call(ctx, p, remotework.Request{Action: "terminal", Runtime: s.state.Runtimes[id], ID: id})
	if e != nil {
		return api.TerminalTarget{}, e
	}
	r.Terminal.SSH, e = s.sshArgs(p, true)
	return r.Terminal, e
}
func (s *Service) refreshCacheLocked(ctx context.Context, id string, p profile, runtime string) {
	r, e := s.call(ctx, p, remotework.Request{Action: "states", Runtime: runtime})
	if e == nil {
		s.cacheResponse(id, p.View.Name, runtime, r.States)
		if p.View.Runtime == runtime && p.View.State == "offline" {
			// Reachability alone is not readiness: recheck native account/model
			// admission before making this machine available for new work.
			_, _ = s.inspectLocked(ctx, id, p.View.Runtime)
		}
	} else {
		if p.View.Runtime == runtime && (p.View.State != "offline" || p.View.Issue != e.Error()) {
			p.View.State, p.View.Issue = "offline", e.Error()
			s.state.Profiles[id] = p
			_ = s.save()
		}
		s.cacheMu.Lock()
		s.cacheRevision[id+"\x00"+runtime]++
		for i := range s.cache[id+"\x00"+runtime] {
			if status := s.cache[id+"\x00"+runtime][i].Task.Status; status != "completed" && status != "failed" && status != "interrupted" && status != "cancelled" {
				s.cache[id+"\x00"+runtime][i].Task.Status = "unknown"
			}
		}
		s.cacheMu.Unlock()
	}
}

// Poll native facts, never a model. Observation cancellation leaves target owners
// alive; reconnecting reads the same durable bindings and never resends a turn.
func (s *Service) Observe(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		var observations sync.WaitGroup
		s.mu.Lock()
		s.reloadLocked()
		s.mu.Unlock()
		limit := make(chan struct{}, 4)
		for _, poll := range s.workPolls() {
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				observations.Wait()
				return
			}
			observations.Go(func() {
				defer func() { <-limit }()
				c, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				s.pollWork(c, poll)
			})
		}
		observations.Wait()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
