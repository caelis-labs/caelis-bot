package machines

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/remotework"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

// Connection configuration is the generation fence for network results. Health
// timestamps and worker defaults may change without changing this route/proof.
func sameBotConfiguration(a, b profile) bool {
	return a.Peer == b.Peer && a.View.ID == b.View.ID && a.View.Name == b.View.Name && a.View.Address == b.View.Address && a.View.Archived == b.View.Archived
}
func botCheckResult(p profile, out workerpeer.Response, err error) profile {
	p.View.CheckedAt = time.Now().UnixMilli()
	p.View.State, p.View.Issue = "offline", ""
	if err != nil {
		p.View.Issue = err.Error()
		if p.View.Issue == "worker_not_ready" {
			p.View.State = "setup"
		}
		return p
	}
	p.View.State = "ready"
	p.View.Runtime, p.View.Available = out.Capabilities.Runtime, []string{out.Capabilities.Runtime}
	p.Peer.Peer, p.Identity, p.View.PeerID = out.Peer, out.Peer, out.Peer
	return p
}
func (s *Service) ConnectBot(ctx context.Context, in api.BotConnectionInput) (api.Machine, error) {
	name := strings.TrimSpace(in.Name)
	if len(name) > 80 {
		return api.Machine{}, errors.New("invalid_name")
	}
	address, err := workerpeer.Address(in.Address)
	if err != nil {
		return api.Machine{}, err
	}
	s.mu.Lock()
	if s.loadErr != nil {
		s.mu.Unlock()
		return api.Machine{}, s.loadErr
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
		s.mu.Unlock()
		return old.View, errors.New("machine_connection_type_conflict")
	}
	if name == "" {
		name = address
		if exists {
			name = old.View.Name
		}
	}
	// A rename is local even while B is unavailable or the connection archived.
	// Explicit reconnect (or re-adding an archived address) performs validation.
	reconnect := in.Reconnect || exists && old.View.Archived && in.ID == ""
	if exists && old.Peer.Address == address && strings.TrimSpace(in.Code) == "" && !reconnect {
		next := old
		next.View.Name = name
		s.state.Profiles[id] = next
		if err = s.save(); err != nil {
			s.state.Profiles[id] = old
		}
		s.mu.Unlock()
		return next.View, err
	}
	pending := old
	if !exists {
		pending.View = api.Machine{ID: id, ConnectionType: "bot", Name: name, Address: address, State: "offline"}
		pending.Peer = workerpeer.Connection{Address: address, Token: rand.Text() + rand.Text()}
	}
	if pending.Peer.Token == "" {
		s.mu.Unlock()
		return old.View, errors.New("original_peer_proof_required")
	}
	// Persist the original proof before a potentially uncertain pairing write.
	if !exists {
		s.state.Profiles[id] = pending
		if err = s.save(); err != nil {
			delete(s.state.Profiles, id)
			s.mu.Unlock()
			return api.Machine{}, err
		}
	}
	profileID := s.state.ProfileID
	s.mu.Unlock()
	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result := workerpeer.PairResult{}
	probe := pending.Peer
	probe.Address = address
	// Existing credentials can repair a changed reachable address, and recover
	// a lost pairing response, without using another one-time code.
	if strings.TrimSpace(in.Code) == "" || pending.Peer.Peer == "" {
		observed, readErr := workerpeer.Call(checkCtx, probe, workerpeer.Request{Action: "info"})
		if readErr == nil && !workerpeer.Compatible(observed.Capabilities) {
			readErr = errors.New("bot_worker_contract_incompatible")
		}
		if readErr == nil {
			result = workerpeer.PairResult{Connection: workerpeer.Connection{Address: address, Peer: observed.Peer, Token: probe.Token}, Capabilities: observed.Capabilities}
		} else if strings.TrimSpace(in.Code) == "" {
			err = readErr
		}
	}
	if result.Peer == "" && err == nil {
		sourceName, _ := os.Hostname()
		if len(sourceName) > 80 || strings.ContainsAny(sourceName, "\r\n\t") {
			sourceName = ""
		}
		result, err = workerpeer.PairNamed(checkCtx, address, in.Code, profileID, sourceName, pending.Peer.Token)
	}
	if err == nil && pending.Peer.Peer != "" && pending.Peer.Peer != result.Peer {
		err = errors.New("bot_peer_identity_changed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.state.Profiles[id]
	if !ok || !sameBotConfiguration(current, pending) {
		return current.View, errors.New("bot_connection_changed")
	}
	next := botCheckResult(current, workerpeer.Response{Peer: result.Peer, Capabilities: result.Capabilities}, err)
	if err == nil {
		next.Peer, next.Identity = result.Connection, result.Peer
		next.View.Name, next.View.Address, next.View.Archived = name, address, false
	}
	s.state.Profiles[id] = next
	if saveErr := s.save(); saveErr != nil {
		s.state.Profiles[id] = current
		return current.View, saveErr
	}
	if err == nil {
		s.renewBotObservationsLocked(id)
	}
	return next.View, err
}
func (s *Service) inspectBot(ctx context.Context, id string, recoverWork, renewObservations bool) (api.Machine, error) {
	s.mu.Lock()
	p, ok := s.state.Profiles[id]
	if !ok || p.View.ConnectionType != "bot" {
		s.mu.Unlock()
		return api.Machine{}, errors.New("bot_connection_unknown")
	}
	s.mu.Unlock()
	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	action := "info"
	if recoverWork {
		action = "recover"
	}
	var out workerpeer.Response
	var err error
	if p.Peer.Token == "" {
		err = errors.New("original_peer_proof_required")
	} else {
		out, err = workerpeer.Call(checkCtx, p.Peer, workerpeer.Request{Action: action})
		if err == nil && !workerpeer.Compatible(out.Capabilities) {
			err = errors.New("bot_worker_contract_incompatible")
		}
	}
	if ctx.Err() != nil {
		return p.View, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.state.Profiles[id]
	if !exists || !sameBotConfiguration(current, p) {
		return current.View, nil // A stale check cannot overwrite a repaired route.
	}
	next := botCheckResult(current, out, err)
	s.state.Profiles[id] = next
	if saveErr := s.save(); saveErr != nil {
		s.state.Profiles[id] = current
		return current.View, saveErr
	}
	if err == nil && renewObservations {
		s.renewBotObservationsLocked(id)
	}
	if recoverWork {
		return next.View, err
	}
	return next.View, nil
}
func (s *Service) renewBotObservationsLocked(id string) {
	seen := map[string]bool{}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	for task, machine := range s.state.Routes {
		if machine == id && !s.state.Reservations[task] {
			key := id + "\x00" + s.state.Runtimes[task]
			if !seen[key] {
				s.cacheRevision[key]++
				seen[key] = true
			}
		}
	}
}
func (s *Service) RecoverBotWork(ctx context.Context, id string) (api.Machine, error) {
	return s.inspectBot(ctx, id, true, true)
}

// Empty connections need freshness too. This bounded read-only check has its
// own cadence and never holds the route lock during network or native readiness.
func (s *Service) observeBotProfiles(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		s.mu.Lock()
		ids := []string{}
		for id, p := range s.state.Profiles {
			if p.View.ConnectionType == "bot" && (p.View.CheckedAt == 0 || time.Since(time.UnixMilli(p.View.CheckedAt)) >= 30*time.Second) {
				ids = append(ids, id)
			}
		}
		s.mu.Unlock()
		var checks sync.WaitGroup
		limit := make(chan struct{}, 4)
		for _, id := range ids {
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				checks.Wait()
				return
			}
			checks.Go(func() {
				defer func() { <-limit }()
				_, _ = s.inspectBot(ctx, id, false, false)
			})
		}
		checks.Wait()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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
	p := s.state.Profiles[s.state.Routes[id]]
	if p.Peer != c || s.state.Runtimes[id] != runtime {
		s.mu.Unlock()
		return api.WorkObservation{}, errors.New("peer_connection_changed")
	}
	view := p.View
	s.storeObservation(id, v, view)
	s.mu.Unlock()
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	return s.observations[id], nil
}
func (s *Service) ValidateWorkFiles(ctx context.Context, id string, files []api.InputFile) error {
	if s.route(id) == "" {
		if p, ok := s.local.(api.WorkInputValidator); ok {
			return p.ValidateWorkFiles(ctx, id, files)
		}
		return nil // Pre-contract local embedders retain native validation.
	}
	c, runtime, err := s.connection(id)
	if err != nil {
		return err
	}
	_, err = workerpeer.Call(ctx, c, workerpeer.Request{Action: "validate_files", ID: id, Runtime: runtime, Message: api.TaskMessage{Files: files}})
	return err
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
type peerSubscription struct {
	cancel     context.CancelFunc
	connection workerpeer.Connection
	runtime    string
}

func (s *Service) peerStreams(ctx context.Context) {
	var streams sync.WaitGroup
	running := map[string]peerSubscription{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer func() {
		for _, stream := range running {
			stream.cancel()
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
		for id, stream := range running {
			target, ok := targets[id]
			if !ok || target.Peer != stream.connection || runtimes[id] != stream.runtime {
				stream.cancel()
				delete(running, id)
			}
		}
		for id, p := range targets {
			if _, exists := running[id]; exists {
				continue
			}
			if len(running) >= 16 {
				break
			}
			streamCtx, cancel := context.WithCancel(ctx)
			runtime := runtimes[id]
			running[id] = peerSubscription{cancel: cancel, connection: p.Peer, runtime: runtime}
			streams.Add(1)
			go func() {
				defer streams.Done()
				for streamCtx.Err() == nil {
					err := workerpeer.Watch(streamCtx, p.Peer, id, func(v api.WorkObservation) { s.applyPeerObservation(id, p, runtime, v) })
					if streamCtx.Err() != nil {
						return
					}
					if err != nil {
						s.mu.Lock()
						current, ok := s.state.Profiles[s.state.Routes[id]]
						if ok && current.Peer == p.Peer && s.state.Runtimes[id] == runtime {
							s.invalidateActivity(p.View.ID, runtime)
						}
						s.mu.Unlock()
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

// Configuration equality is the subscription generation. Hold the route lock
// through cache application: both stale success and stale error are fenced.
func (s *Service) applyPeerObservation(id string, p profile, runtime string, v api.WorkObservation) {
	if !workerpeer.Compatible(v.Owner) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.state.Profiles[s.state.Routes[id]]
	if !ok || current.Peer != p.Peer || s.state.Runtimes[id] != runtime {
		return
	}
	s.storeObservation(id, v, current.View)
	state := scopeState(v.State)
	state.Runtime = runtime
	state.Task.Machine = current.View.ID
	state.Task.MachineName = current.View.Name
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	key := current.View.ID + "\x00" + runtime
	for i := range s.cache[key] {
		if s.cache[key][i].Task.ID == id {
			s.cache[key][i] = state
			s.cacheRevision[key]++
			return
		}
	}
	s.cache[key] = append(s.cache[key], state)
	s.cacheRevision[key]++
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
