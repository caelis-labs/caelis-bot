package machines

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

func sameBotConfiguration(a, b profile) bool {
	return a.PairingEpoch == b.PairingEpoch && a.Peer == b.Peer && a.View.ID == b.View.ID && a.View.Name == b.View.Name && a.View.Address == b.View.Address && a.View.Archived == b.View.Archived
}
func botCheckResult(p profile, out workerpeer.Response, err error) profile {
	p.View.CheckedAt = time.Now().UnixMilli()
	p.View.State, p.View.Issue = "offline", ""
	if validPeerDisplayName(out.Name) {
		p.View.RemoteName = out.Name
	}
	if err != nil {
		p.View.Issue = err.Error()
		if p.View.Issue == "worker_not_ready" {
			p.View.State = "setup"
		}
		return p
	}
	p.View.State = "ready"
	p.View.Runtime = out.Capabilities.Runtime
	p.Peer.Peer = out.Peer
	p.View.PeerID = out.Peer
	return p
}

// Profile health is sampled only for established grants. Pending authorization
// is checked on page entry or explicit Inspect, never by a background poll.
func (s *Service) observeMachineProfiles(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		ids := []string{}
		s.mu.Lock()
		for id, p := range s.state.Profiles {
			if p.View.PairingStatus == "approved" && (p.View.CheckedAt == 0 || time.Since(time.UnixMilli(p.View.CheckedAt)) >= 30*time.Second) {
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
			checks.Go(func() { defer func() { <-limit }(); _, _ = s.inspectBot(ctx, id, false, false) })
		}
		checks.Wait()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type peerWorkResponse struct {
	Task         api.Task
	Messages     []api.TaskConversationMessage
	States       []api.WorkState
	Runtime      string
	Capabilities api.WorkCapabilities
	Peer         string
}

func (s *Service) call(ctx context.Context, p profile, in workerpeer.Request) (peerWorkResponse, error) {
	if in.Action == "inspect" {
		in.Action = "info"
	}
	if in.Action == "start" {
		caps, err := s.peerCall(ctx, p.Peer, workerpeer.Request{Action: "admission", ID: in.Start.ID, Runtime: in.Runtime})
		if err != nil {
			return peerWorkResponse{}, err
		}
		if !workerpeer.Compatible(caps.Capabilities) {
			return peerWorkResponse{}, errors.New("original_worker_contract_incompatible")
		}
	}
	out, err := s.peerCall(ctx, p.Peer, in)
	if err != nil {
		return peerWorkResponse{}, err
	}
	result := peerWorkResponse{Task: out.Task, Messages: out.Messages, Runtime: out.Capabilities.Runtime, Capabilities: out.Capabilities, Peer: out.Peer}
	if in.Action == "info" && !workerpeer.Compatible(out.Capabilities) {
		return result, errors.New("bot_worker_contract_incompatible")
	}
	for _, state := range out.States {
		result.States = append(result.States, scopeState(workerpeer.DecodeState(state)))
	}
	return result, nil
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
	if s.observationAt == nil {
		s.observationAt = map[string]time.Time{}
	}
	s.observations[id] = v
	s.observationAt[id] = time.Now()
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
	out, err := s.peerCall(ctx, c, workerpeer.Request{Action: "observe", ID: id, Runtime: runtime})
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
	_, err = s.peerCall(ctx, c, workerpeer.Request{Action: "validate_files", ID: id, Runtime: runtime, Message: api.TaskMessage{Files: files}})
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
	out, err := s.peerCall(ctx, c, workerpeer.Request{Action: "stop", Runtime: runtime, Stop: in})
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
	// A peer control reply cannot retain this host's update drain for the Web
	// ingress's longer general lifetime. This is the existing peer control/read
	// deadline; cancellation preserves the original native request/receipt.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	_, err = s.peerCall(ctx, c, workerpeer.Request{Action: "decision", Runtime: runtime, Decision: in})
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
	out, err := s.peerCall(ctx, c, workerpeer.Request{Action: "url", ID: id, Runtime: runtime, Ref: request})
	if err == nil {
		u, parseErr := url.Parse(out.URL)
		if parseErr != nil {
			return "", &api.ActionRejectedError{Code: "remote_native_url_invalid"}
		}
		ip := net.ParseIP(u.Hostname())
		if strings.EqualFold(u.Hostname(), "localhost") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".localhost") || (ip != nil && (ip.IsLoopback() || ip.IsUnspecified())) {
			return "", &api.ActionRejectedError{Code: "remote_native_url_not_reachable"}
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
	out, err := s.peerCall(ctx, c, workerpeer.Request{Action: "operation", Runtime: runtime, Query: in})
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
	out, err := s.peerCall(ctx, c, workerpeer.Request{Action: "file", Runtime: runtime, ID: id, Ref: ref})
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
	_, err = s.peerCall(ctx, c, workerpeer.Request{Action: "closure", Runtime: runtime, Closure: in})
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
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	owned := map[string]bool{}
	for _, states := range s.cache {
		for _, state := range states {
			owned[state.Task.ID] = true
		}
	}
	remote := []api.Approval{}
	for id, v := range s.observations {
		if owned[id] {
			remote = append(remote, v.Approvals...)
		}
	}
	slices.SortFunc(remote, func(a, b api.Approval) int { return strings.Compare(a.ID, b.ID) })
	fingerprint := api.WorkFingerprint(remote)
	if fingerprint != s.peerApprovalFingerprint {
		s.peerApprovalFingerprint = fingerprint
		s.peerApprovalRevision++
	}
	// Renderer observations require a monotonic revision. A native content hash
	// identifies changes but its numeric value can decrease or exceed JS precision.
	return append(out, remote...), revision + s.peerApprovalRevision
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
	done       <-chan struct{}
	cancel     context.CancelFunc
	connection workerpeer.Connection
	runtime    string
}

func (s *Service) peerStreams(ctx context.Context) {
	var streams sync.WaitGroup
	running := map[string]peerSubscription{}
	attempted := map[string]time.Time{}
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
		reapPeerStreams(running, targets, runtimes)
		for id, p := range targets {
			if _, exists := running[id]; exists {
				continue
			}
			if len(running) >= 16 {
				break
			}
			streamCtx, cancel := context.WithCancel(ctx)
			runtime := runtimes[id]
			done := make(chan struct{})
			running[id] = peerSubscription{done: done, cancel: cancel, connection: p.Peer, runtime: runtime}
			streams.Add(1)
			go func() {
				defer streams.Done()
				defer close(done)
				for streamCtx.Err() == nil {
					err := s.peerWatch(streamCtx, p.Peer, id, func(v api.WorkObservation) { s.applyPeerObservation(id, p, runtime, v) })
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
		// Streams improve latency, not interaction completeness. Observe stale
		// or unsubscribed tasks fairly with four bounded reads per round. Failed
		// reads rotate too, so four unreachable tasks cannot starve the rest.
		s.observePeerFallback(ctx, targets, runtimes, attempted)
	}
}

func (s *Service) observePeerFallback(ctx context.Context, targets map[string]profile, runtimes map[string]string, attempted map[string]time.Time) {
	type candidate struct {
		id string
		at time.Time
	}
	now := time.Now()
	due := []candidate{}
	s.cacheMu.RLock()
	for id := range targets {
		at := s.observationAt[id]
		if attempted[id].After(at) {
			at = attempted[id]
		}
		if now.Sub(at) >= 2*time.Second {
			due = append(due, candidate{id, at})
		}
	}
	s.cacheMu.RUnlock()
	for id := range attempted {
		if _, ok := targets[id]; !ok {
			delete(attempted, id)
		}
	}
	slices.SortFunc(due, func(a, b candidate) int {
		if a.at.Before(b.at) {
			return -1
		}
		if a.at.After(b.at) {
			return 1
		}
		return strings.Compare(a.id, b.id)
	})
	if len(due) > 4 {
		due = due[:4]
	}
	var reads sync.WaitGroup
	for _, entry := range due {
		id := entry.id
		attempted[id] = now
		p, runtime := targets[id], runtimes[id]
		reads.Go(func() {
			readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			out, err := s.peerCall(readCtx, p.Peer, workerpeer.Request{Action: "observe", ID: id, Runtime: runtime})
			if err == nil {
				s.applyPeerObservation(id, p, runtime, workerpeer.DecodeObservation(out.Observation))
			}
		})
	}
	reads.Wait()
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
				if state.Activity == "active" {
					return true
				}
				observation, observed := s.observations[id]
				if !observed {
					return true
				}
				for _, approval := range observation.Approvals {
					if approval.Status == "pending" {
						return true
					}
				}
				// AsyncQuestions are completed native call records. Their user
				// lifecycle/closure is retained elsewhere and a nonempty history
				// does not imply a live native producer needing a stream. State
				// polling still imports these records for A's question owner.
				return false
			default:
				return true
			}
		}
	}
	return true
}

func reapPeerStreams(running map[string]peerSubscription, targets map[string]profile, runtimes map[string]string) {
	for id, stream := range running {
		// A task can resume on the same connection while its canceled Watch exits.
		// Reap completed lifetimes even if the target becomes eligible again.
		select {
		case <-stream.done:
			delete(running, id)
			continue
		default:
		}
		target, ok := targets[id]
		if !ok || target.Peer != stream.connection || runtimes[id] != stream.runtime {
			stream.cancel()
		}
	}
}

// Routing classification only; native decision validation remains authoritative.
func (s *Service) RemoteWorkApproval(id string) bool {
	ref, ok := unscopedRef("worker-approval:", id)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Routes[ref.Task] != ""
}

func validPeerDisplayName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 80 && !strings.ContainsFunc(name, unicode.IsControl)
}

// RemoteName is optional display metadata. Keep the persisted original intent
// name unchanged, so a dropped pair reply still reconciles its original proof.
func machineView(p profile) api.Machine {
	v := p.View
	if v.ConnectionType == "bot" && v.Name == v.Address && validPeerDisplayName(v.RemoteName) {
		v.Name = v.RemoteName
	}
	return v
}
