package machines

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/remotework"
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
		return "", e
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
	p := s.state.Profiles[in.Machine]
	r, e := s.call(ctx, p, remotework.Request{Action: "start", Runtime: runtime, Start: in})
	r.Task.Machine = in.Machine
	r.Task.MachineName = p.View.Name
	s.refreshCacheLocked(ctx, in.Machine, p, runtime)
	return r.Task, e
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
		s.mu.Lock()
		seen := map[string]bool{}
		for task, id := range s.state.Routes {
			runtime := s.state.Runtimes[task]
			key := id + "\x00" + runtime
			if !seen[key] {
				seen[key] = true
				if p, ok := s.state.Profiles[id]; ok {
					c, cancel := context.WithTimeout(ctx, 15*time.Second)
					s.refreshCacheLocked(c, id, p, runtime)
					cancel()
				}
			}
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
