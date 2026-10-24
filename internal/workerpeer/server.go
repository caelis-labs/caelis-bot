package workerpeer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
)

type Owner interface {
	api.WorkRuntime
	Connect(context.Context) error
	Close(context.Context) error
}

// Factory reads B's own runtime/model settings. runtime is empty only for a new
// binding or readiness check. Caller identity comes exclusively from auth.
type Factory func(context.Context, string, string, string) (Owner, string, *api.WorkExecutionSettings, error)
type authenticatedGrant struct{ Peer, Hash string }
type authenticatedGrantKey struct{}

type originalBindingOnlyKey struct{}

// OriginalBindingOnly resolves the existing source/runtime owner without current
// model defaults or preparation. Only fixed authenticated receipt/guarded-stop
// actions set this context; native stop still checks its original generation.
func OriginalBindingOnly(ctx context.Context) bool {
	v, _ := ctx.Value(originalBindingOnlyKey{}).(bool)
	return v
}

type grant struct {
	Peer     string `json:"peer"`
	Hash     string `json:"hash"`
	Revoked  bool   `json:"revoked,omitempty"`
	Name     string `json:"name,omitempty"`
	PairedAt int64  `json:"pairedAt,omitempty"`
}
type registry struct {
	Version      int                          `json:"version"`
	Identity     string                       `json:"identity"`
	Enabled      bool                         `json:"enabled"`
	Grants       map[string]grant             `json:"grants"`
	Bindings     map[string]map[string]string `json:"bindings"`               // source/task -> frozen runtime
	Preparations map[string]map[string]bool   `json:"preparations,omitempty"` // no native start attempted
}
type Server struct {
	stageMu sync.RWMutex
	mu      sync.Mutex
	root    string
	state   registry
	factory Factory
	ready   func(context.Context) error
	owners  map[string]Owner
	code    string
	expires time.Time
}
type Sharing struct {
	Enabled   bool                   `json:"enabled"`
	Address   string                 `json:"address"`
	Peer      string                 `json:"peer"`
	Code      string                 `json:"code,omitempty"`
	ExpiresAt int64                  `json:"expiresAt,omitempty"`
	Sources   []api.WorkerPeerSource `json:"sources"`
}

func Open(root string, factory Factory) (*Server, error) {
	s := &Server{root: root, factory: factory, owners: map[string]Owner{}, state: registry{Version: 1, Identity: rand.Text(), Grants: map[string]grant{}, Bindings: map[string]map[string]string{}}}
	b, err := os.ReadFile(filepath.Join(root, "peers.json"))
	if err == nil {
		if json.Unmarshal(b, &s.state) != nil || s.state.Version != 1 || s.state.Identity == "" || s.state.Grants == nil || s.state.Bindings == nil {
			return nil, errors.New("peer_registry_invalid")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	if s.state.Preparations == nil {
		s.state.Preparations = map[string]map[string]bool{}
	}
	if err = s.save(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Server) save() error { return localstate.Write(filepath.Join(s.root, "peers.json"), s.state) }

// ConfigureReadiness reuses the host's configured local Worker inspection. Code
// generation never creates a separate, empty foreign execution owner.
func (s *Server) ConfigureReadiness(check func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = check
}

func (s *Server) checkReadiness(ctx context.Context) error {
	s.mu.Lock()
	enabled, ready := s.state.Enabled, s.ready
	s.mu.Unlock()
	if !enabled {
		return errors.New("worker_sharing_disabled")
	}
	if ready != nil {
		return ready(ctx)
	}
	return ctx.Err()
}
func (s *Server) Status() Sharing {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := Sharing{Enabled: s.state.Enabled, Peer: s.state.Identity, Sources: []api.WorkerPeerSource{}}
	for peer, g := range s.state.Grants {
		if g.Hash != "" && !g.Revoked {
			v.Sources = append(v.Sources, api.WorkerPeerSource{ID: peer, Name: g.Name, PairedAt: g.PairedAt})
		}
	}
	slices.SortFunc(v.Sources, func(a, b api.WorkerPeerSource) int { return strings.Compare(a.ID, b.ID) })
	return v
}
func (s *Server) Enable(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.state.Enabled
	s.state.Enabled = enabled
	s.code = ""
	if err := s.save(); err != nil {
		s.state.Enabled = old
		return err
	}
	return nil
}
func (s *Server) Pairing(ctx context.Context) (Sharing, error) {
	if err := s.checkReadiness(ctx); err != nil {
		return Sharing{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.Enabled {
		return Sharing{}, errors.New("worker_sharing_disabled")
	}
	s.code = rand.Text()
	s.expires = time.Now().Add(5 * time.Minute)
	return Sharing{Enabled: true, Peer: s.state.Identity, Code: s.code, ExpiresAt: s.expires.Unix()}, nil
}

// Explicit recovery prepares new work only. The adapter alone can verify that
// an original native process is gone and preserve its old task receipts.
func recoverNewWork(ctx context.Context, owner Owner) error {
	if p, ok := owner.(interface{ RecoverNewWork(context.Context) error }); ok {
		if err := p.RecoverNewWork(ctx); err != nil {
			return err
		}
	} else if err := owner.Connect(ctx); err != nil {
		return err
	}
	return owner.WorkAdmission(ctx)
}
func (s *Server) RecoverReadiness(ctx context.Context) error {
	return s.checkReadiness(ctx)
}
func (s *Server) Revoke(peer string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.state.Grants[peer]
	if !ok {
		return errors.New("peer_unknown")
	}
	if len(s.state.Bindings[peer]) == 0 {
		delete(s.state.Grants, peer)
	} else {
		revoked := old
		revoked.Revoked = true
		s.state.Grants[peer] = revoked
	}
	if err := s.save(); err != nil {
		s.state.Grants[peer] = old
		return err
	}
	return nil
}
func tokenHash(v string) string    { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
func sourceDir(peer string) string { return tokenHash(peer) }
func validID(id string) bool {
	if len(id) < 1 || len(id) > 160 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func (s *Server) owner(ctx context.Context, peer, id, runtime string, newBinding bool) (Owner, *api.WorkExecutionSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	proof, ok := ctx.Value(authenticatedGrantKey{}).(authenticatedGrant)
	current, valid := s.state.Grants[peer]
	if !ok || !valid || current.Revoked || current.Hash != proof.Hash || proof.Peer != peer || !s.state.Enabled {
		return nil, nil, errors.New("peer_authorization_revoked")
	}
	routes := s.state.Bindings[peer]
	if routes == nil {
		routes = map[string]string{}
	}
	bound, exists := routes[id]
	if !exists && !newBinding {
		return nil, nil, errors.New("worker_not_owned")
	}
	if exists {
		if runtime != "" && runtime != bound {
			return nil, nil, errors.New("worker_owner_conflict")
		}
		runtime = bound
	} else {
		runtime = ""
	}
	root := filepath.Join(s.root, "sources", sourceDir(peer))
	candidate, selected, work, err := s.factory(ctx, peer, runtime, root)
	if err != nil {
		return nil, nil, err
	}
	if selected != "codex" && selected != "caelis" {
		return nil, nil, errors.New("worker_runtime_unavailable")
	}
	if exists && selected != bound {
		return nil, nil, errors.New("original_worker_owner_unavailable")
	}
	key := peer + "\x00" + selected
	owner := s.owners[key]
	if owner == nil {
		owner = candidate
		s.owners[key] = owner
	}
	if !exists {
		routes[id] = selected
		s.state.Bindings[peer] = routes
		if s.state.Preparations[peer] == nil {
			s.state.Preparations[peer] = map[string]bool{}
		}
		s.state.Preparations[peer][id] = true
		if err = s.save(); err != nil {
			delete(routes, id)
			delete(s.state.Preparations[peer], id)
			return nil, nil, err
		}
	}
	return owner, work, nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// The peer endpoint never accepts browser cookies or loopback trust. A browser
	// Origin cannot become an authenticated host-to-host request through SSH.
	if r.Header.Get("Origin") != "" {
		http.Error(w, "peer_browser_origin_denied", http.StatusForbidden)
		return
	}
	if r.URL.Path == Prefix+"pair" {
		s.pair(w, r)
		return
	}
	s.mu.Lock()
	enabled := s.state.Enabled
	identity := s.state.Identity
	peer := ""
	revoked := false
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		hash := tokenHash(strings.TrimPrefix(header, "Bearer "))
		for _, g := range s.state.Grants {
			if g.Hash == hash {
				revoked = g.Revoked
				peer = g.Peer
				break
			}
		}
	}
	s.mu.Unlock()
	if peer != "" && revoked {
		http.Error(w, "peer_authorization_revoked", http.StatusForbidden)
		return
	}
	if !enabled {
		http.Error(w, "worker_sharing_disabled", http.StatusForbidden)
		return
	}
	if peer == "" {
		http.Error(w, "peer_authentication_required", http.StatusUnauthorized)
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), authenticatedGrantKey{}, authenticatedGrant{Peer: peer, Hash: tokenHash(strings.TrimPrefix(header, "Bearer "))}))
	if r.URL.Path == Prefix+"events" {
		s.events(w, r, peer)
		return
	}
	if r.Method != "POST" || r.URL.Path != Prefix+"call" {
		http.NotFound(w, r)
		return
	}
	// Only authenticated Worker requests receive the bounded attachment budget;
	// browser/headless defaults and all peer authentication remain unchanged.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(api.WorkerAttachmentTimeout))
	var in Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 230<<20)).Decode(&in); err != nil {
		http.Error(w, "invalid_worker_request", 400)
		return
	}
	out := Response{Contract: api.WorkerContract, Peer: identity}
	if in.Contract != api.WorkerContract {
		out.Error = "worker_contract_incompatible"
	} else {
		out = s.dispatch(r.Context(), peer, in)
		out.Contract, out.Peer = api.WorkerContract, identity
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	var in PairRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || !validID(in.Peer) || len(in.Token) < 40 || len(in.Token) > 128 || !validID(in.Token) {
		http.Error(w, "invalid_peer", 400)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) > 80 || strings.ContainsFunc(in.Name, unicode.IsControl) {
		http.Error(w, "invalid_peer_name", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.Enabled {
		http.Error(w, "worker_sharing_disabled", 403)
		return
	}
	if s.code != "" && time.Now().After(s.expires) {
		http.Error(w, "pairing_code_expired", 403)
		return
	}
	if s.code == "" || tokenHash(in.Code) != tokenHash(s.code) {
		http.Error(w, "pairing_code_invalid", 403)
		return
	}
	if existing, exists := s.state.Grants[in.Peer]; exists {
		if existing.Hash == "" || tokenHash(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) != existing.Hash {
			http.Error(w, "original_peer_proof_required", 403)
			return
		}
	} else if len(s.state.Bindings[in.Peer]) > 0 {
		http.Error(w, "original_peer_revoked", 403)
		return
	}
	owner, _, _, err := s.factory(r.Context(), in.Peer, "", filepath.Join(s.root, "sources", sourceDir(in.Peer)))
	if err == nil {
		err = recoverNewWork(r.Context(), owner)
	}
	var caps api.WorkCapabilities
	if err == nil {
		p, ok := owner.(api.WorkCapabilityProvider)
		if !ok {
			err = errors.New("worker_contract_unavailable")
		} else {
			caps, err = p.WorkCapabilities(r.Context(), "")
		}
	}
	if err == nil && !Compatible(caps) {
		err = errors.New("worker_contract_incompatible")
	}
	if err != nil {
		http.Error(w, "worker_not_ready: "+err.Error(), 409)
		return
	}
	token := in.Token
	old, exists := s.state.Grants[in.Peer]
	if exists {
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	pairedAt := old.PairedAt
	if pairedAt == 0 {
		pairedAt = time.Now().UnixMilli()
	}
	name := in.Name
	if name == "" {
		name = old.Name
	}
	s.state.Grants[in.Peer] = grant{Peer: in.Peer, Hash: tokenHash(token), Name: name, PairedAt: pairedAt}
	if err = s.save(); err != nil {
		if exists {
			s.state.Grants[in.Peer] = old
		} else {
			delete(s.state.Grants, in.Peer)
		}
		http.Error(w, "pairing_not_saved", 500)
		return
	}
	s.code = ""
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(PairResult{Connection: Connection{Peer: s.state.Identity, Token: token}, Capabilities: caps})
}
func (s *Server) dispatch(ctx context.Context, peer string, in Request) (out Response) {
	fail := func(err error) Response {
		out.Error = err.Error()
		var message *api.WorkMessageRejection
		var decision *api.DecisionValidationError
		var decisionValue api.DecisionValidationError
		out.Rejected = errors.As(err, &message) || errors.As(err, &decision) || errors.As(err, &decisionValue)
		return out
	}
	if in.Action == "unpair" {
		s.mu.Lock()
		defer s.mu.Unlock()
		proof, ok := ctx.Value(authenticatedGrantKey{}).(authenticatedGrant)
		current, valid := s.state.Grants[peer]
		if !ok || !valid || current.Revoked || current.Hash != proof.Hash || proof.Peer != peer || !s.state.Enabled {
			return fail(errors.New("peer_authorization_revoked"))
		}
		for id := range s.state.Bindings[peer] {
			if !s.state.Preparations[peer][id] {
				return fail(errors.New("peer_has_worker_bindings"))
			}
		}
		old := s.state.Grants[peer]
		bindings, preparations := s.state.Bindings[peer], s.state.Preparations[peer]
		delete(s.state.Grants, peer)
		delete(s.state.Bindings, peer)
		delete(s.state.Preparations, peer)
		if err := s.save(); err != nil {
			s.state.Grants[peer] = old
			s.state.Bindings[peer], s.state.Preparations[peer] = bindings, preparations
			return fail(err)
		}
		return
	}
	if in.Action == "info" || in.Action == "recover" {
		owner, _, _, err := s.factory(ctx, peer, "", filepath.Join(s.root, "sources", sourceDir(peer)))
		if err != nil {
			return fail(fmt.Errorf("worker_not_ready: %w", err))
		}
		if in.Action == "recover" {
			err = recoverNewWork(ctx, owner)
		} else {
			err = owner.Connect(ctx)
		}
		if err != nil {
			return fail(fmt.Errorf("worker_not_ready: %w", err))
		}
		p, ok := owner.(api.WorkCapabilityProvider)
		if !ok {
			return fail(errors.New("worker_contract_unavailable"))
		}
		out.Capabilities, err = p.WorkCapabilities(ctx, "")
		if err != nil {
			return fail(err)
		}
		if in.Action == "info" {
			if err = owner.WorkAdmission(ctx); err != nil {
				return fail(fmt.Errorf("worker_not_ready: %w", err))
			}
		}
		return
	}
	if in.Action == "states" {
		s.mu.Lock()
		ids := []string{}
		for id := range s.state.Bindings[peer] {
			ids = append(ids, id)
		}
		s.mu.Unlock()
		for _, id := range ids {
			owner, _, err := s.owner(ctx, peer, id, "", false)
			if err != nil {
				out.States = append(out.States, encodeState(api.WorkState{Task: api.Task{ID: id, Status: "unknown"}}))
				continue
			}
			connectErr := owner.Connect(ctx)
			found := false
			for _, state := range owner.WorkStates() {
				if state.Task.ID == id {
					found = true
					if connectErr != nil {
						if state.Activity != api.WorkActivityOwnerUnavailable {
							state.Activity = ""
						}
						if state.Task.Status != "completed" && state.Task.Status != "failed" && state.Task.Status != "interrupted" && state.Task.Status != "cancelled" {
							state.Task.Status = "unknown"
						}
					}
					out.States = append(out.States, encodeState(state))
				}
			}
			if !found {
				out.States = append(out.States, encodeState(api.WorkState{Task: api.Task{ID: id, Status: "unknown"}}))
			}
		}
		return
	}
	id := in.ID
	switch in.Action {
	case "start":
		id = in.Start.ID
	case "send":
		id = in.Message.ID
	case "stop":
		id = in.Stop.ID
	case "decision":
		id = in.Decision.TaskID
	case "url-submit":
		id = in.URLSubmission.TaskID
	case "operation":
		id = in.Query.TaskID
	case "closure":
		id = in.Closure.WorkID
	}
	if !validID(id) {
		return fail(errors.New("invalid_worker_id"))
	}
	if in.Action == "release_preparation" {
		if err := s.releasePreparation(peer, id); err != nil {
			return fail(err)
		}
		return
	}
	// Preparation validation must precede durable binding admission. A bad
	// workspace cannot create an execution owner or strand pairing credentials.
	if in.Action == "prepare" {
		workspace := in.Start.TaskStart.Workspace
		if workspace != "" && !filepath.IsAbs(workspace) {
			return fail(errors.New("worker_workspace_not_absolute"))
		}
	}
	if in.Action == "url-operation" || in.Action == "operation" || in.Action == "stop" {
		ctx = context.WithValue(ctx, originalBindingOnlyKey{}, true)
	}
	owner, work, err := s.owner(ctx, peer, id, in.Runtime, in.Action == "prepare" || in.Action == "start")
	if err != nil {
		return fail(err)
	}
	// A durable original receipt is readable even when its native process or
	// request is gone. Authenticated source/binding lookup above is still required;
	// this read must not connect, recover, or substitute an execution owner.
	if in.Action == "operation" {
		p, ok := owner.(api.WorkOperationReader)
		if !ok {
			return fail(api.ErrWorkOperationUnsupported)
		}
		out.Operation, err = p.ReadWorkOperation(ctx, in.Query)
		if err != nil {
			return fail(err)
		}
		return
	}
	if in.Action == "url-operation" {
		p, ok := owner.(api.WorkURLChallenge)
		if !ok {
			return fail(errors.New("worker_url_page_unsupported"))
		}
		out.Operation, err = p.ReadWorkURLSubmission(ctx, id, in.Ref, in.Query.RequestID)
		if err != nil {
			return fail(err)
		}
		return
	}
	if in.Action == "prepare" {
		err = recoverNewWork(ctx, owner)
	} else if in.Action != "stop" {
		err = owner.Connect(ctx)
	}
	if err != nil {
		return fail(err)
	}
	if in.Action == "validate_files" {
		if len(in.Files) > 0 {
			return fail(errors.New("worker_preflight_requires_metadata"))
		}
		p, ok := owner.(api.WorkCapabilityProvider)
		if !ok {
			return fail(errors.New("worker_input_validation_unsupported"))
		}
		s.mu.Lock()
		prepared := s.state.Preparations[peer][id]
		s.mu.Unlock()
		original := id
		if prepared {
			original = ""
		}
		caps, e := p.WorkCapabilities(ctx, original)
		if e != nil {
			return fail(e)
		}
		if e = api.ValidateWorkFileMetadata(in.InputMetadata, caps); e != nil {
			return fail(e)
		}
		out.Capabilities = caps
		return
	}
	if in.Action == "prepare" {
		workspace := in.Start.TaskStart.Workspace
		if workspace == "" {
			workspace = filepath.Join(s.root, "sources", sourceDir(peer), "tasks", id)
		}
		if !filepath.IsAbs(workspace) {
			return fail(errors.New("worker_workspace_not_absolute"))
		}
		if err = os.MkdirAll(workspace, 0700); err != nil {
			return fail(err)
		}
		out.Task = api.Task{ID: id, Workspace: workspace}
		if p, ok := owner.(api.WorkCapabilityProvider); ok {
			out.Capabilities, err = p.WorkCapabilities(ctx, "")
			if err != nil {
				return fail(err)
			}
		}
		return
	}
	if in.Action != "start" && in.Action != "admission" {
		p, ok := owner.(api.WorkCapabilityProvider)
		if !ok {
			return fail(errors.New("original_worker_contract_unsupported"))
		}
		caps, err := p.WorkCapabilities(ctx, id)
		if err != nil {
			return fail(err)
		}
		if !Compatible(caps) {
			return fail(errors.New("original_worker_contract_incompatible"))
		}
	}
	if in.Action == "start" || in.Action == "send" {
		files, cleanup, err := s.stage(ctx, peer, id, in.Files)
		if err != nil {
			return fail(err)
		}
		// Both shared native adapters synchronously copy/retain accepted input
		// before returning. These are only per-call transport bytes, including
		// on rejection/cancellation/unknown; original native/A copies stay owned.
		defer cleanup()
		if in.Action == "start" {
			in.Start.Files = files
			in.Start.Machine = ""
			in.Start.ExecutionOverride = work
		} else {
			in.Message.Files = files
		}
	}
	switch in.Action {
	case "start":
		// Crossing the native boundary can have an unknown effect. Only this fence
		// converts a removable preparation into a retained execution binding.
		s.mu.Lock()
		proof, authenticated := ctx.Value(authenticatedGrantKey{}).(authenticatedGrant)
		grant, granted := s.state.Grants[peer]
		if !authenticated || !granted || grant.Revoked || !s.state.Enabled || grant.Hash != proof.Hash || proof.Peer != peer || s.state.Bindings[peer][id] == "" {
			s.mu.Unlock()
			return fail(errors.New("peer_authorization_revoked"))
		}
		wasPrepared := s.state.Preparations[peer][id]
		delete(s.state.Preparations[peer], id)
		if saveErr := s.save(); saveErr != nil {
			if wasPrepared {
				s.state.Preparations[peer][id] = true
			}
			s.mu.Unlock()
			return fail(saveErr)
		}
		s.mu.Unlock()
		out.Task, err = owner.StartWork(ctx, in.Start)
	case "read":
		out.Task, err = owner.ReadWork(ctx, id)
	case "send":
		out.Task, err = owner.SendWork(ctx, in.Message)
	case "stop":
		p, ok := owner.(api.WorkStopper)
		if !ok {
			return fail(errors.New("worker_guarded_stop_unsupported"))
		}
		out.Task, err = p.StopWorkRequest(ctx, in.Stop)
	case "observe":
		p, ok := owner.(api.WorkObserver)
		if !ok {
			return fail(errors.New("worker_observe_unsupported"))
		}
		var v api.WorkObservation
		v, err = p.ObserveWork(ctx, id)
		out.Observation = encodeObservation(v)
	case "admission":
		p, ok := owner.(api.WorkCapabilityProvider)
		if !ok {
			return fail(errors.New("worker_contract_unsupported"))
		}
		if err = owner.WorkAdmission(ctx); err == nil {
			out.Capabilities, err = p.WorkCapabilities(ctx, "")
		}
	case "capabilities":
		p, ok := owner.(api.WorkCapabilityProvider)
		if !ok {
			return fail(errors.New("worker_contract_unsupported"))
		}
		out.Capabilities, err = p.WorkCapabilities(ctx, id)
	case "messages":
		p, ok := owner.(api.WorkConversationReader)
		if !ok {
			return fail(errors.New("worker_history_unsupported"))
		}
		out.Messages, err = p.ReadWorkConversation(ctx, id)
	case "decision":
		p, ok := owner.(api.WorkDecider)
		if !ok {
			return fail(errors.New("worker_interactions_unsupported"))
		}
		err = p.DecideWork(ctx, in.Decision)
	case "url":
		p, ok := owner.(api.WorkDecider)
		if !ok {
			return fail(errors.New("worker_url_unsupported"))
		}
		out.URL, err = p.WorkApprovalURL(ctx, id, in.Ref)
	case "url-page", "url-submit":
		p, ok := owner.(api.WorkURLChallenge)
		if !ok {
			return fail(errors.New("worker_url_page_unsupported"))
		}
		switch in.Action {
		case "url-page":
			out.URLPage, err = p.ReadWorkURL(ctx, id, in.Ref)
		case "url-submit":
			out.Operation, err = p.SubmitWorkURL(ctx, in.URLSubmission)
		}
	case "operation":
		p, ok := owner.(api.WorkOperationReader)
		if !ok {
			return fail(errors.New("worker_reconcile_unsupported"))
		}
		out.Operation, err = p.ReadWorkOperation(ctx, in.Query)
	case "file":
		p, ok := owner.(api.WorkFileReader)
		if !ok {
			return fail(errors.New("worker_files_unsupported"))
		}
		out.File, err = p.ReadWorkFile(ctx, id, in.Ref)
	case "closure":
		p, ok := owner.(api.QuestionClosureReceiver)
		if !ok {
			return fail(errors.New("worker_closure_unsupported"))
		}
		err = p.ReceiveQuestionClosure(ctx, in.Closure)
	default:
		err = errors.New("worker_operation_unsupported")
	}
	if err != nil {
		return fail(err)
	}
	return
}
func (s *Server) stage(ctx context.Context, peer, id string, files []api.WorkFileContent) (out []api.InputFile, cleanup func(), err error) {
	cleanup = func() {}
	if len(files) > 8 {
		return nil, cleanup, errors.New("too_many_worker_files")
	}
	for _, file := range files {
		if file.Name == "" || file.Name == "." || file.Name == ".." || file.Name != filepath.Base(file.Name) || strings.ContainsAny(file.Name, "/\\\x00") || len(file.Data) > api.MaxWorkFileBytes {
			return nil, cleanup, errors.New("invalid_worker_file")
		}
	}
	if len(files) == 0 {
		return []api.InputFile{}, cleanup, nil
	}
	if err = lockwait.RLock(ctx, &s.stageMu); err != nil {
		return nil, cleanup, err
	}
	var once sync.Once
	dir := ""
	cleanup = func() {
		once.Do(func() {
			if dir != "" {
				_ = os.RemoveAll(dir)
			}
			s.stageMu.RUnlock()
		})
	}
	defer func() {
		if err != nil {
			cleanup()
		}
	}()
	root := filepath.Join(s.root, "uploads")
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, cleanup, err
	}
	dir, err = os.MkdirTemp(root, sourceDir(peer)+"-"+id+"-")
	if err != nil {
		return nil, cleanup, err
	}
	for i, file := range files {
		part := filepath.Join(dir, fmt.Sprint(i))
		if err = os.Mkdir(part, 0700); err != nil {
			return nil, cleanup, err
		}
		path := filepath.Join(part, file.Name)
		if err = os.WriteFile(path, file.Data, 0600); err != nil {
			return nil, cleanup, err
		}
		out = append(out, api.InputFile{Name: file.Name, Path: path})
	}
	return out, cleanup, nil
}

func (s *Server) releasePreparation(peer, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.Preparations[peer][id] {
		return nil
	}
	runtime := s.state.Bindings[peer][id]
	delete(s.state.Preparations[peer], id)
	delete(s.state.Bindings[peer], id)
	if err := s.save(); err != nil {
		s.state.Preparations[peer][id] = true
		s.state.Bindings[peer][id] = runtime
		return err
	}
	return nil
}
func (s *Server) events(w http.ResponseWriter, r *http.Request, peer string) {
	id := r.URL.Query().Get("id")
	if r.Method != "GET" || !validID(id) {
		http.Error(w, "invalid_worker", 400)
		return
	}
	if _, _, err := s.owner(r.Context(), peer, id, "", false); err != nil {
		http.Error(w, "worker_not_owned", 403)
		return
	}
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream_unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	last := ""
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		s.mu.Lock()
		g, valid := s.state.Grants[peer]
		enabled := s.state.Enabled
		s.mu.Unlock()
		if !valid || g.Revoked || g.Hash != tokenHash(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) || !enabled {
			return
		}
		v := s.dispatch(r.Context(), peer, Request{Action: "observe", ID: id})
		v.Contract = api.WorkerContract
		s.mu.Lock()
		v.Peer = s.state.Identity
		s.mu.Unlock()
		b, _ := json.Marshal(v)
		hash := tokenHash(string(b))
		if hash != last {
			if _, err := fmt.Fprintf(w, "event: worker\ndata: %s\n\n", b); err != nil {
				return
			}
			f.Flush()
			last = hash
		}
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}
func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	owners := s.owners
	s.owners = map[string]Owner{}
	s.mu.Unlock()
	var err error
	for _, owner := range owners {
		if d, ok := owner.(interface{ DetachForUpdate(context.Context) error }); ok {
			err = errors.Join(err, d.DetachForUpdate(ctx))
		} else {
			err = errors.Join(err, owner.Close(ctx))
		}
	}
	return err
}

// Reuse the existing attachment inventory/retention policy for interrupted
// transport leftovers. Active calls retain the read fence until native input
// ownership has transferred; cleanup never scans native owner or source files.
func (s *Server) InputStorage(ctx context.Context, clean bool, trash func(string) error) (api.AttachmentStorage, error) {
	if err := lockwait.WriteLock(ctx, &s.stageMu); err != nil {
		return api.AttachmentStorage{}, err
	}
	defer s.stageMu.Unlock()
	return workerfiles.PlainStorage(ctx, filepath.Join(s.root, "uploads"), nil, clean, trash)
}
