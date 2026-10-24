package machines

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/remotework"
	"github.com/caelis-labs/caelis-bot/internal/sharedruntime"
	"reflect"
	"sync"
	"time"
)

func (s *Service) TaskMachines() []api.TaskMachine {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []api.TaskMachine{}
	for _, p := range s.state.Profiles {
		// New product delegation uses Bot peers. Legacy helper profiles remain
		// addressable only through already retained task bindings and setup repair.
		if p.View.ConnectionType != "bot" || p.View.Archived {
			continue
		}
		out = append(out, api.TaskMachine{ID: p.View.ID, Name: p.View.Name, Runtime: p.View.Runtime, Ready: botProfileReady(p.View)})
	}
	return out
}
func botProfileReady(v api.Machine) bool {
	age := time.Since(time.UnixMilli(v.CheckedAt))
	return v.State == "ready" && !v.Archived && v.CheckedAt > 0 && age >= 0 && age <= 60*time.Second
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
	if p.View.ConnectionType == "bot" && len(r.Available) == 1 {
		selected := r.Available[0]
		if selected != "codex" && selected != "caelis" {
			return "", errors.New("original_task_runtime_unavailable")
		}
		s.state.Runtimes[id] = selected
		if err := s.save(); err != nil {
			return "", err
		}
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
	runtime, e := s.bindLocked(in.TaskStart, in.ID, "")
	if e != nil {
		s.mu.Unlock()
		return api.Task{}, e
	}
	// Retain the original route before dispatch, even if the response is lost.
	if s.state.Reservations[in.ID] {
		delete(s.state.Reservations, in.ID)
		if e = s.save(); e != nil {
			s.state.Reservations[in.ID] = true
			s.mu.Unlock()
			return api.Task{}, e
		}
	}
	p := s.state.Profiles[in.Machine]
	s.mu.Unlock()
	return s.callWork(ctx, in.Machine, runtime, p, remotework.Request{Action: "start", Runtime: runtime, Start: in})
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
			s.cacheMu.RLock()
			revision := s.cacheRevision[key]
			s.cacheMu.RUnlock()
			out = append(out, workPoll{id, runtime, p, revision})
		}
	}
	return out
}

// Network observation must never own the global route/configuration lock.
// Apply only to the unchanged profile and still-owned backend; a late response
// cannot restore a deleted machine or overwrite a newly selected default.
func (s *Service) pollWork(ctx context.Context, poll workPoll) uint64 {
	r, err := s.call(ctx, poll.profile, remotework.Request{Action: "states", Runtime: poll.runtime})
	var inspected remotework.Response
	var inspectErr error
	recheck := err == nil && poll.profile.View.Runtime == poll.runtime && poll.profile.View.State == "offline"
	if recheck {
		inspected, inspectErr = s.call(ctx, poll.profile, remotework.Request{Action: "inspect", Runtime: poll.runtime})
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return poll.revision
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, exists := s.state.Profiles[poll.id]
	s.cacheMu.RLock()
	revision := s.cacheRevision[poll.id+"\x00"+poll.runtime]
	s.cacheMu.RUnlock()
	if !exists || !reflect.DeepEqual(p, poll.profile) || revision != poll.revision {
		return poll.revision
	}
	owned := false
	for task, machine := range s.state.Routes {
		if machine == poll.id && s.state.Runtimes[task] == poll.runtime && !s.state.Reservations[task] {
			owned = true
			break
		}
	}
	if !owned {
		return poll.revision
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
		return poll.revision + 1
	}
	if p.View.Runtime == poll.runtime && (p.View.State != "offline" || p.View.Issue != err.Error()) {
		next := p
		next.View.State, next.View.Issue = "offline", err.Error()
		s.state.Profiles[poll.id] = next
		if s.save() != nil {
			s.state.Profiles[poll.id] = p
		}
	}
	s.invalidateActivity(poll.id, poll.runtime)
	return poll.revision + 1
}

// Every failed original-owner observation invalidates activity evidence,
// regardless of whether it came from foreground readback or background polling.
func (s *Service) invalidateActivity(id, runtime string) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	key := id + "\x00" + runtime
	s.cacheRevision[key]++
	for i := range s.cache[key] {
		state := &s.cache[key][i]
		state.Activity = ""
		if status := state.Task.Status; status != "completed" && status != "failed" && status != "interrupted" && status != "cancelled" {
			state.Task.Status = "unknown"
		}
	}
}
func (s *Service) remote(ctx context.Context, id, action string, message api.TaskMessage) (api.Task, error) {
	s.mu.Lock()
	machine, runtime := s.state.Routes[id], s.state.Runtimes[id]
	p, ok := s.state.Profiles[machine]
	s.mu.Unlock()
	if !ok {
		return api.Task{}, errors.New("original_machine_unavailable")
	}
	return s.callWork(ctx, machine, runtime, p, remotework.Request{Action: action, Runtime: runtime, ID: id, Message: message})
}

// A network write must not hold the route lock in front of its guarded Stop.
// Refresh uses the existing profile/revision fence, so late results cannot
// overwrite observations from a changed connection or another execution.
func (s *Service) callWork(ctx context.Context, machine, runtime string, p profile, request remotework.Request) (api.Task, error) {
	s.cacheMu.RLock()
	revision := s.cacheRevision[machine+"\x00"+runtime]
	s.cacheMu.RUnlock()
	r, err := s.call(ctx, p, request)
	r.Task.Machine, r.Task.MachineName = machine, p.View.Name
	s.pollWork(ctx, workPoll{id: machine, runtime: runtime, profile: p, revision: revision})
	return r.Task, err
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
func (s *Service) ReadWorkConversation(ctx context.Context, id string) ([]api.TaskConversationMessage, error) {
	if s.route(id) == "" {
		reader, ok := s.local.(api.WorkConversationReader)
		if !ok {
			return nil, errors.New("native local task messages unavailable")
		}
		return reader.ReadWorkConversation(ctx, id)
	}
	s.mu.Lock()
	machine, runtime := s.state.Routes[id], s.state.Runtimes[id]
	profile, ok := s.state.Profiles[machine]
	s.mu.Unlock()
	if !ok || runtime == "" {
		return nil, errors.New("original remote task environment unavailable")
	}
	response, err := s.call(ctx, profile, remotework.Request{Action: "messages", Runtime: runtime, ID: id})
	if err != nil {
		return nil, err
	}
	return response.Messages, nil
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
		s.mu.Lock()
		machine, runtime := s.state.Routes[in.ID], s.state.Runtimes[in.ID]
		profile, ok := s.state.Profiles[machine]
		s.mu.Unlock()
		if !ok || runtime == "" {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		response, err := s.call(ctx, profile, remotework.Request{Action: "recorded", Runtime: runtime, ID: in.ID, Message: in})
		return err == nil && response.Recorded
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

// Poll native facts, never a model. Observation cancellation leaves target owners
// alive; reconnecting reads the same durable bindings and never resends a turn.
func (s *Service) Observe(ctx context.Context) {
	go s.peerStreams(ctx)
	go s.observeBotProfiles(ctx)
	type observation struct {
		budget   sharedruntime.ObservationBudget
		revision uint64
	}
	episodes := map[string]*observation{}
	var episodeMu sync.Mutex

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
			key := poll.id + "\x00" + poll.runtime
			episodeMu.Lock()
			episode := episodes[key]
			if episode == nil {
				episode = &observation{}
				episodes[key] = episode
			}
			if episode.revision != poll.revision {
				episode.budget.Reset()
			}
			allowed := episode.budget.Take(time.Now())
			episodeMu.Unlock()
			if !allowed {
				continue
			}

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
				observed := s.pollWork(c, poll)
				s.cacheMu.RLock()
				revision := s.cacheRevision[key]
				active := false
				for _, state := range s.cache[key] {
					if state.Activity == "active" || state.Task.Status == "working" || state.Task.Status == "running" || state.Task.Status == "pending" || state.Task.Status == "waiting_approval" || state.Task.Status == "awaiting_approval" {
						active = true
					}
				}
				s.cacheMu.RUnlock()
				episodeMu.Lock()
				// Adopt only the revision this poll actually wrote. An explicit
				// recovery/read racing the poll must still grant a fresh budget.
				episode.revision = observed
				if revision == observed && active {
					episode.budget.Reset()
				}
				episodeMu.Unlock()
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
