package machines

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/remotework"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

func (s *Service) ConnectBot(ctx context.Context, in api.BotConnectionInput) (api.Machine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return api.Machine{}, s.loadErr
	}
	if len(strings.TrimSpace(in.Name)) > 80 {
		return api.Machine{}, errors.New("invalid_name")
	}
	address, err := workerpeer.Address(in.Address)
	if err != nil {
		return api.Machine{}, err
	}
	id := in.ID
	if id == "" {
		for key, p := range s.state.Profiles {
			if p.View.ConnectionType == "bot" && p.Peer.Address == address {
				id = key
				break
			}
		}
	}
	if id == "" {
		id = "bot-" + rand.Text()
	}
	old, exists := s.state.Profiles[id]
	if exists && old.View.ConnectionType != "bot" {
		return old.View, errors.New("machine_connection_type_conflict")
	}
	pending := old
	if !exists {
		pending.View = api.Machine{ID: id, ConnectionType: "bot", Name: in.Name, Address: address, State: "offline"}
		pending.Peer = workerpeer.Connection{Address: address, Token: rand.Text() + rand.Text()}
	}
	// Persist the exact credential before pairing. If the response disappears,
	// the next attempt can read that original grant without minting a new source.
	if pending.Peer.Token == "" {
		return old.View, errors.New("original_peer_credential_unavailable")
	}
	if !exists {
		s.state.Profiles[id] = pending
		if err = s.save(); err != nil {
			delete(s.state.Profiles, id)
			return api.Machine{}, err
		}
	}
	result := workerpeer.PairResult{}
	if pending.Peer.Peer == "" {
		probe := pending.Peer
		probe.Address = address
		if observed, readErr := workerpeer.Call(ctx, probe, workerpeer.Request{Action: "info"}); readErr == nil && workerpeer.Compatible(observed.Capabilities) {
			result = workerpeer.PairResult{Connection: workerpeer.Connection{Address: address, Peer: observed.Peer, Token: probe.Token}, Capabilities: observed.Capabilities}
		}
	}
	if result.Peer == "" {
		result, err = workerpeer.Pair(ctx, address, in.Code, s.state.ProfileID, pending.Peer.Token)
		if err != nil {
			return pending.View, err
		}
	}
	if exists && old.Peer.Peer != "" && old.Peer.Peer != result.Peer {
		return old.View, errors.New("target_identity_changed")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = in.Address
	}
	p := profile{Peer: result.Connection, Identity: result.Peer, View: api.Machine{ID: id, ConnectionType: "bot", PeerID: result.Peer, Name: name, Address: result.Address, Runtime: result.Capabilities.Runtime, Available: []string{result.Capabilities.Runtime}, State: "ready"}}
	s.state.Profiles[id] = p
	if err = s.save(); err != nil {
		if exists {
			s.state.Profiles[id] = old
		} else {
			delete(s.state.Profiles, id)
		}
		return api.Machine{}, err
	}
	return p.View, nil
}
func (s *Service) peerCall(ctx context.Context, p profile, in remotework.Request) (remotework.Response, error) {
	action := in.Action
	if action == "inspect" || action == "detect" {
		action = "info"
	}
	switch action {
	case "info", "states", "prepare", "start", "read", "send", "messages":
	default:
		return remotework.Response{}, errors.New("bot_peer_operation_unsupported")
	}
	req := workerpeer.Request{Action: action, ID: in.ID, Runtime: in.Runtime, Start: in.Start, Message: in.Message}
	if action == "start" {
		caps, err := workerpeer.Call(ctx, p.Peer, workerpeer.Request{Action: "admission", ID: in.Start.ID, Runtime: in.Runtime})
		if err != nil {
			return remotework.Response{}, err
		}
		if !workerpeer.Compatible(caps.Capabilities) {
			return remotework.Response{}, errors.New("original_worker_contract_incompatible")
		}
	}
	out, err := workerpeer.Call(ctx, p.Peer, req)
	if err != nil {
		return remotework.Response{}, err
	}
	result := remotework.Response{Identity: out.Peer, Task: out.Task, Messages: out.Messages}
	if action == "prepare" {
		result.Available = []string{out.Capabilities.Runtime}
	}
	if action == "info" {
		if !workerpeer.Compatible(out.Capabilities) {
			return result, errors.New("bot_worker_contract_incompatible")
		}
		result.Available = []string{out.Capabilities.Runtime}
		result.Setup = api.SetupState{State: "ready"}
	}
	for _, state := range out.States {
		v := workerpeer.DecodeState(state)
		result.States = append(result.States, scopeState(v))
		observation, err := workerpeer.Call(ctx, p.Peer, workerpeer.Request{Action: "observe", ID: v.Task.ID, Runtime: in.Runtime})
		if err == nil {
			s.storeObservation(v.Task.ID, workerpeer.DecodeObservation(observation.Observation), p.View)
		}
	}
	return result, nil
}

type interactionRef struct {
	Task   string
	Native string
}

func scopedRef(prefix, task, native string) string {
	b, _ := json.Marshal(interactionRef{task, native})
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}
func unscopedRef(prefix, id string) (interactionRef, bool) {
	if !strings.HasPrefix(id, prefix) {
		return interactionRef{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, prefix))
	var out interactionRef
	if err != nil || json.Unmarshal(b, &out) != nil || out.Task == "" || out.Native == "" {
		return interactionRef{}, false
	}
	return out, true
}
func scopeState(state api.WorkState) api.WorkState {
	state.AsyncQuestions = slices.Clone(state.AsyncQuestions)
	for i := range state.AsyncQuestions {
		state.AsyncQuestions[i].ID = scopedRef("worker-question:", state.Task.ID, state.AsyncQuestions[i].ID)
	}
	return state
}
func (s *Service) storeObservation(id string, v api.WorkObservation, metadata ...api.Machine) {
	if len(metadata) > 0 {
		v.State.Task.Machine = metadata[0].ID
		v.State.Task.MachineName = metadata[0].Name
		v.State.Runtime = v.Owner.Runtime
	}
	v.State = scopeState(v.State)
	v.Approvals = slices.Clone(v.Approvals)
	for i := range v.Approvals {
		a := &v.Approvals[i]
		if a.NativeID == "" {
			a.NativeID = a.ID
		}
		a.ID = scopedRef("worker-approval:", id, a.ID)
		a.TaskID = id
	}
	s.cacheMu.Lock()
	if s.observations == nil {
		s.observations = map[string]api.WorkObservation{}
	}
	s.observations[id] = v
	s.cacheMu.Unlock()
}
func (s *Service) connection(id string) (workerpeer.Connection, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	machine := s.state.Routes[id]
	p, ok := s.state.Profiles[machine]
	if !ok || p.View.ConnectionType != "bot" {
		return workerpeer.Connection{}, "", errors.New("original_bot_peer_unavailable")
	}
	return p.Peer, s.state.Runtimes[id], nil
}
func (s *Service) ObserveWork(ctx context.Context, id string) (api.WorkObservation, error) {
	if s.route(id) == "" {
		p, ok := s.local.(api.WorkObserver)
		if !ok {
			return api.WorkObservation{}, errors.New("worker_observe_unsupported")
		}
		return p.ObserveWork(ctx, id)
	}
	c, runtime, err := s.connection(id)
	if err != nil {
		return api.WorkObservation{}, err
	}
	out, err := workerpeer.Call(ctx, c, workerpeer.Request{Action: "observe", ID: id, Runtime: runtime})
	if err != nil {
		return api.WorkObservation{}, err
	}
	v := workerpeer.DecodeObservation(out.Observation)
	if !workerpeer.Compatible(v.Owner) {
		return api.WorkObservation{}, errors.New("original_worker_contract_incompatible")
	}
	s.mu.Lock()
	view := s.state.Profiles[s.state.Routes[id]].View
	s.mu.Unlock()
	s.storeObservation(id, v, view)
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	return s.observations[id], nil
}
func (s *Service) StopWorkRequest(ctx context.Context, in api.WorkStop) (api.Task, error) {
	if s.route(in.ID) == "" {
		if p, ok := s.local.(api.WorkStopper); ok {
			return p.StopWorkRequest(ctx, in)
		}
		return s.local.StopWork(ctx, in.ID)
	}
	c, runtime, err := s.connection(in.ID)
	if err != nil { // Legacy SSH routes deliberately retain their original contract.
		s.mu.Lock()
		p := s.state.Profiles[s.state.Routes[in.ID]]
		s.mu.Unlock()
		if p.View.ConnectionType == "bot" {
			return api.Task{}, err
		}
		return s.remote(ctx, in.ID, "stop", api.TaskMessage{})
	}
	out, err := workerpeer.Call(ctx, c, workerpeer.Request{Action: "stop", Runtime: runtime, Stop: in})
	out.Task.Machine = s.route(in.ID)
	s.mu.Lock()
	out.Task.MachineName = s.state.Profiles[out.Task.Machine].View.Name
	s.mu.Unlock()
	return out.Task, err
}
func (s *Service) DecideWork(ctx context.Context, in api.WorkDecision) error {
	if s.route(in.TaskID) == "" {
		p, ok := s.local.(api.WorkDecider)
		if !ok {
			return errors.New("worker_interactions_unsupported")
		}
		return p.DecideWork(ctx, in)
	}
	c, runtime, err := s.connection(in.TaskID)
	if err != nil {
		return err
	}
	if ref, ok := unscopedRef("worker-approval:", in.Decision.ID); ok {
		if ref.Task != in.TaskID {
			return errors.New("worker_request_source_conflict")
		}
		in.Decision.ID = ref.Native
	}
	_, err = workerpeer.Call(ctx, c, workerpeer.Request{Action: "decision", Runtime: runtime, Decision: in})
	return err
}
func (s *Service) WorkApprovalURL(ctx context.Context, id, request string) (string, error) {
	if s.route(id) == "" {
		p, ok := s.local.(api.WorkDecider)
		if !ok {
			return "", errors.New("worker_url_unsupported")
		}
		return p.WorkApprovalURL(ctx, id, request)
	}
	c, runtime, err := s.connection(id)
	if err != nil {
		return "", err
	}
	if ref, ok := unscopedRef("worker-approval:", request); ok {
		if ref.Task != id {
			return "", errors.New("worker_request_source_conflict")
		}
		request = ref.Native
	}
	out, err := workerpeer.Call(ctx, c, workerpeer.Request{Action: "url", ID: id, Runtime: runtime, Ref: request})
	if err == nil {
		u, parseErr := url.Parse(out.URL)
		if parseErr != nil {
			return "", errors.New("remote_native_url_invalid")
		}
		ip := net.ParseIP(u.Hostname())
		if strings.EqualFold(u.Hostname(), "localhost") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".localhost") || (ip != nil && (ip.IsLoopback() || ip.IsUnspecified())) {
			return "", errors.New("remote_native_url_not_reachable")
		}
	}
	return out.URL, err
}
func (s *Service) ReadWorkOperation(ctx context.Context, in api.WorkOperationQuery) (api.WorkOperation, error) {
	if s.route(in.TaskID) == "" {
		p, ok := s.local.(api.WorkOperationReader)
		if !ok {
			return api.WorkOperation{}, errors.New("worker_reconcile_unsupported")
		}
		return p.ReadWorkOperation(ctx, in)
	}
	c, runtime, err := s.connection(in.TaskID)
	if err != nil {
		return api.WorkOperation{}, api.ErrWorkOperationUnsupported
	}
	out, err := workerpeer.Call(ctx, c, workerpeer.Request{Action: "operation", Runtime: runtime, Query: in})
	return out.Operation, err
}
func (s *Service) ReadWorkFile(ctx context.Context, id, ref string) (api.WorkFileContent, error) {
	if s.route(id) == "" {
		p, ok := s.local.(api.WorkFileReader)
		if !ok {
			return api.WorkFileContent{}, errors.New("worker_files_unsupported")
		}
		return p.ReadWorkFile(ctx, id, ref)
	}
	c, runtime, err := s.connection(id)
	if err != nil {
		return api.WorkFileContent{}, err
	}
	out, err := workerpeer.Call(ctx, c, workerpeer.Request{Action: "file", Runtime: runtime, ID: id, Ref: ref})
	return out.File, err
}
func (s *Service) ReceiveQuestionClosure(ctx context.Context, in api.QuestionClosure) error {
	if s.route(in.WorkID) == "" {
		p, ok := s.local.(api.QuestionClosureReceiver)
		if !ok {
			return errors.New("worker_closure_unsupported")
		}
		return p.ReceiveQuestionClosure(ctx, in)
	}
	c, runtime, err := s.connection(in.WorkID)
	if err != nil {
		return err
	}
	if ref, ok := unscopedRef("worker-question:", in.ItemID); ok {
		if ref.Task != in.WorkID {
			return errors.New("worker_request_source_conflict")
		}
		in.ItemID = ref.Native
	}
	_, err = workerpeer.Call(ctx, c, workerpeer.Request{Action: "closure", Runtime: runtime, Closure: in})
	return err
}

// These methods are the existing retained interaction hook, now fed by both
// local and remote Workers. The Agent's filtered view is still built in app.
func (s *Service) RetainedApprovalState() ([]api.Approval, uint64) {
	out := []api.Approval{}
	var revision uint64
	if p, ok := s.local.(interface {
		RetainedApprovalState() ([]api.Approval, uint64)
	}); ok {
		out, revision = p.RetainedApprovalState()
	}
	s.cacheMu.RLock()
	owned := map[string]bool{}
	for _, states := range s.cache {
		for _, state := range states {
			owned[state.Task.ID] = true
		}
	}
	defer s.cacheMu.RUnlock()
	for id, v := range s.observations {
		if owned[id] {
			out = append(out, v.Approvals...)
			var part uint64
			for _, b := range []byte(id + v.Revision) {
				part = part*31 + uint64(b)
			}
			revision += part
		}
	}
	return out, revision
}
func (s *Service) DecideRetainedApproval(ctx context.Context, d api.Decision) (bool, error) {
	if ref, ok := unscopedRef("worker-approval:", d.ID); ok {
		native := d
		native.ID = ref.Native
		return true, s.DecideWork(ctx, api.WorkDecision{TaskID: ref.Task, RequestID: api.WorkDecisionRequestID(ref.Task, native), Decision: d})
	}
	if p, ok := s.local.(interface {
		DecideRetainedApproval(context.Context, api.Decision) (bool, error)
	}); ok {
		return p.DecideRetainedApproval(ctx, d)
	}
	return false, nil
}
func (s *Service) RetainedApprovalURL(id string) (string, bool, error) {
	if ref, ok := unscopedRef("worker-approval:", id); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		url, err := s.WorkApprovalURL(ctx, ref.Task, id)
		return url, true, err
	}
	if p, ok := s.local.(interface {
		RetainedApprovalURL(string) (string, bool, error)
	}); ok {
		return p.RetainedApprovalURL(id)
	}
	return "", false, nil
}

// peerStreams uses the same bounded native observation contract as local owners.
// Each reconnect reads a fresh snapshot; it never dispatches a task or decision.
func (s *Service) peerStreams(ctx context.Context) {
	var streams sync.WaitGroup
	running := map[string]context.CancelFunc{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer func() {
		for _, cancel := range running {
			cancel()
		}
		streams.Wait()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.mu.Lock()
		targets := map[string]profile{}
		runtimes := map[string]string{}
		for id, machine := range s.state.Routes {
			p := s.state.Profiles[machine]
			if p.View.ConnectionType == "bot" && !s.state.Reservations[id] && s.needsPeerStream(id) {
				targets[id] = p
				runtimes[id] = s.state.Runtimes[id]
			}
		}
		s.mu.Unlock()
		for id, cancel := range running {
			if _, ok := targets[id]; !ok {
				cancel()
				delete(running, id)
			}
		}
		for id, p := range targets {
			if len(running) >= 16 {
				break
			}
			if running[id] != nil {
				continue
			}
			streamCtx, cancel := context.WithCancel(ctx)
			running[id] = cancel
			runtime := runtimes[id]
			streams.Add(1)
			go func() {
				defer streams.Done()
				for streamCtx.Err() == nil {
					err := workerpeer.Watch(streamCtx, p.Peer, id, func(v api.WorkObservation) {
						if !workerpeer.Compatible(v.Owner) {
							return
						}
						s.mu.Lock()
						current, ok := s.state.Profiles[s.state.Routes[id]]
						same := ok && current.Peer == p.Peer && s.state.Runtimes[id] == runtime
						s.mu.Unlock()
						if !same {
							return
						}
						s.storeObservation(id, v, p.View)
						state := scopeState(v.State)
						state.Runtime = runtime
						state.Task.Machine = p.View.ID
						state.Task.MachineName = p.View.Name
						s.cacheMu.Lock()
						key := p.View.ID + "\x00" + runtime
						found := false
						for i := range s.cache[key] {
							if s.cache[key][i].Task.ID == id {
								s.cache[key][i] = state
								found = true
								break
							}
						}
						if !found {
							s.cache[key] = append(s.cache[key], state)
						}
						s.cacheRevision[key]++
						s.cacheMu.Unlock()
					})
					if streamCtx.Err() != nil {
						return
					}
					if err != nil {
						s.invalidateActivity(p.View.ID, runtime)
					}
					select {
					case <-streamCtx.Done():
						return
					case <-time.After(time.Second):
					}
				}
			}()
		}
	}
}

// Completed history stays on demand. A bounded number of live subscriptions
// prevents retained task history from becoming an ever-growing event service.
func (s *Service) needsPeerStream(id string) bool {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	for _, states := range s.cache {
		for _, state := range states {
			if state.Task.ID != id {
				continue
			}
			switch state.Task.Status {
			case "completed", "failed", "cancelled", "interrupted", "stopped":
				if len(state.AsyncQuestions) > 0 {
					return true
				}
				for _, approval := range s.observations[id].Approvals {
					if approval.Status == "pending" {
						return true
					}
				}
				return false
			default:
				return true
			}
		}
	}
	return true
}
