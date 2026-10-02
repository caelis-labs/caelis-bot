package caelis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// WorkerEndpoint is native bootstrap state. Enroll runs where the Host owner
// credential lives; only the independent application credential crosses SSH.
// No credential is serialized, displayed, or exposed through the Worker port.
type WorkerEndpoint struct {
	Capabilities                             []string
	Execution                                api.WorkExecutionSettings
	ModelConfigured                          bool
	ModelAuth                                string
	Origin, StoreID, InstanceID, PrincipalID string
	Enroll                                   func(context.Context, string, string) (wire.ApplicationConnection, error) `json:"-"`
}
type WorkerProtocol string

const (
	WorkerProtocolSharedNative       WorkerProtocol = "shared-native-worker"
	WorkerProtocolBoundedApplication WorkerProtocol = "bounded-application-worker"
)

func workerProtocol(v WorkerProtocol) (WorkerProtocol, error) {
	if v == "" {
		return WorkerProtocolSharedNative, nil
	}
	if v != WorkerProtocolSharedNative && v != WorkerProtocolBoundedApplication {
		return "", errors.New("Worker protocol unavailable")
	}
	return v, nil
}

// Protocol is trusted native assembly policy, never task/model/renderer input.
// The empty value preserves already pinned shared-native Worker semantics.
type WorkerOptions struct {
	Protocol  WorkerProtocol
	Target    api.WorkTarget
	Directory string
	Execution api.WorkExecutionSettings
	Endpoint  func(context.Context) (WorkerEndpoint, error)
	Source    api.WorkSourceProvider
	Workspace api.WorkWorkspaceProvider
}

// WorkerClient has no resident, submission, callback, or Host lifecycle API.
// The private Session value shares journals/projection code, never a main Session.
type WorkerClient struct {
	engine    *Session
	workspace api.WorkWorkspaceProvider
}

var workerRequired = []string{"shared-native-workers-v1", "turn-steering-receipts-v1", "application-runtime-v1", "application-resource-transfer-v1"}

var boundedWorkerRequired = []string{"application-runtime-v1", "application-native-execution-v1", "application-workspace-binding-v1", "application-background-activation-v1", "application-resource-transfer-v1", "execution-configuration-v1", "turn-steering-receipts-v1"}

func workerProtocolCapabilities(v WorkerProtocol) []string {
	if v == WorkerProtocolBoundedApplication {
		return boundedWorkerRequired
	}
	return workerRequired
}

func NewWorker(opts WorkerOptions) *WorkerClient {
	s := New(Options{Directory: opts.Directory, WorkExecution: opts.Execution})
	s.path = filepath.Join(opts.Directory, "worker-application.json")
	s.state, s.loadErr = loadBinding(s.path)
	if !filepath.IsAbs(opts.Directory) {
		s.loadErr = errors.New("private absolute Worker directory required")
	}
	s.workerOnly, s.workerEndpoint, s.workerTarget = true, opts.Endpoint, opts.Target
	protocol, protocolErr := workerProtocol(opts.Protocol)
	if protocolErr != nil {
		s.loadErr = protocolErr
	}
	s.workerProtocol = protocol
	s.workerUseDefault = opts.Execution.Model == ""
	if opts.Source != nil {
		s.workerSource = opts.Source.WorkDispatchSource
	}
	return &WorkerClient{engine: s, workspace: opts.Workspace}
}
func (s *Session) initialize(ctx context.Context, c *client) (wire.ServerInfo, error) {
	if s.workerOnly {
		return initializeCapabilities(ctx, c, workerProtocolCapabilities(s.workerProtocol))
	}
	return initialize(ctx, c)
}
func (s *Session) validWorkWorkspace(v string) bool {
	if !s.workerOnly {
		return filepath.IsAbs(v)
	}
	// Remote paths were resolved by the target. Never evaluate them on this OS.
	return strings.HasPrefix(v, "/") && path.Clean(v) == v && !strings.ContainsRune(v, 0)
}

type workerCredential struct {
	credential
	Protocol   WorkerProtocol
	Target     api.WorkTarget
	InstanceID string
}

func workerSecretPath(v string) string {
	return filepath.Join(filepath.Dir(v), "worker-application-credential.json")
}
func (s *Session) connectWorker(ctx context.Context) error {
	if s.workerEndpoint == nil {
		return errors.New("Worker endpoint unavailable")
	}
	ep, e := s.workerEndpoint(ctx)
	if e != nil {
		return e
	}
	for _, capability := range workerProtocolCapabilities(s.workerProtocol) {
		if !slices.Contains(ep.Capabilities, capability) {
			return errors.New("Worker public capability unavailable")
		}
	}
	if ep.StoreID == "" || ep.InstanceID == "" || ep.PrincipalID == "" || ep.Enroll == nil {
		return errors.New("Worker endpoint identity unavailable")
	}
	s.mu.Lock()
	if s.workerUseDefault {
		s.workExecution = ep.Execution
	}
	s.workerModelConfigured = ep.ModelConfigured && s.workExecution.Model != "" && s.workExecution.Model == ep.Execution.Model
	s.workerModelAuth = ep.ModelAuth
	oldBinding := s.state
	s.mu.Unlock()
	if oldBinding.Session.SessionId != "" || oldBinding.StoreID != "" && (oldBinding.StoreID != ep.StoreID || oldBinding.PrincipalID != ep.PrincipalID) {
		return errors.New("Worker Host identity changed; original binding retained")
	}
	if err := s.workerTarget.Validate(); err != nil || s.workerTarget.Backend != "caelis" || s.workerTarget.Role != api.RoleWorker {
		return errors.New("Worker route target unavailable")
	}
	var key workerCredential
	raw, e := privateRead(workerSecretPath(s.path), 65536)
	fresh := errors.Is(e, os.ErrNotExist)
	if fresh {
		secret := make([]byte, 32)
		if _, e = rand.Read(secret); e != nil {
			return e
		}
		key = workerCredential{credential: credential{StoreID: ep.StoreID, PrincipalID: ep.PrincipalID, OperationID: "register-worker-" + rand.Text(), Token: "app-client-" + hex.EncodeToString(secret)}, Protocol: s.workerProtocol, Target: s.workerTarget, InstanceID: ep.InstanceID}
		if e = privateWrite(workerSecretPath(s.path), key); e != nil {
			return e
		}
	} else if e != nil {
		return e
	} else if json.Unmarshal(raw, &key) != nil || key.StoreID != ep.StoreID || key.PrincipalID != ep.PrincipalID || key.Token == "" || key.OperationID == "" || key.Target != s.workerTarget {
		return errors.New("Worker credential binding mismatch")
	}
	pinnedProtocol, protocolErr := workerProtocol(key.Protocol)
	if protocolErr != nil || pinnedProtocol != s.workerProtocol {
		return errors.New("Worker protocol changed; original binding retained")
	}
	c, e := newClient(ep.Origin, key.Token)
	if e != nil {
		return e
	}

	ok := false
	defer func() {
		if !ok {
			c.http.CloseIdleConnections()
		}
	}()
	var life wire.ApplicationConnection
	if !fresh {
		e = c.json(ctx, "GET", "/application/connection", nil, &life, "", "")
	}
	if fresh || isRemoteStatus(e, 401) {
		life, e = ep.Enroll(ctx, key.OperationID, key.Token)
	}
	if e != nil {
		return e
	}
	if life.Revoked || life.PrincipalId != ep.PrincipalID || life.ApplicationId == "" || life.ConnectionId == "" {
		return errors.New("Worker enrollment unavailable or revoked")
	}
	old := oldBinding.Connection
	if old.ConnectionId != "" && (old.ConnectionId != life.ConnectionId || old.ApplicationId != life.ApplicationId || old.PrincipalId != life.PrincipalId) {
		return errors.New("Worker enrollment changed; original binding retained")
	}
	if len(oldBinding.Workers) > 0 && s.workerProtocol == WorkerProtocolSharedNative {
		var grants []wire.ApplicationWorker
		if e = c.json(ctx, "GET", "/application/workers", nil, &grants, "", ""); e != nil {
			return e
		}
		for _, worker := range oldBinding.Workers {
			if !worker.Native {
				return errors.New("Worker protocol does not match original task")
			}
			if worker.Binding.SessionId == "" {
				continue
			}
			owned := false
			for _, grant := range grants {
				if grant.SessionId == worker.Binding.SessionId && grant.ApplicationId == life.ApplicationId && grant.ConnectionId == life.ConnectionId && grant.PrincipalId == life.PrincipalId && worker.Binding.ApplicationId == grant.ApplicationId && worker.Binding.ConnectionId == grant.ConnectionId && worker.Binding.PrincipalId == grant.PrincipalId {
					owned = true
					break
				}
			}
			if !owned {
				return errors.New("Worker original native grant unavailable")
			}
		}
	}
	if len(oldBinding.Workers) > 0 && s.workerProtocol == WorkerProtocolBoundedApplication {
		for _, worker := range oldBinding.Workers {
			if worker.Native {
				return errors.New("Worker protocol does not match original task")
			}
			if worker.Binding.SessionId == "" {
				continue
			}
			var binding wire.ApplicationBinding
			if e = c.json(ctx, "GET", "/application/sessions/"+idPath(worker.Binding.SessionId), nil, &binding, "", ""); e != nil {
				return e
			}
			if binding.Archived || binding.SessionId != worker.Binding.SessionId || binding.ApplicationId != life.ApplicationId || binding.ConnectionId != life.ConnectionId || binding.PrincipalId != life.PrincipalId || binding.CreationDigest != worker.Binding.CreationDigest {
				return errors.New("Worker original bounded binding unavailable")
			}
		}
	}
	info, e := s.initialize(ctx, c)
	if e != nil {
		return e
	}
	if value(info.StoreId) != ep.StoreID || value(info.InstanceId) != ep.InstanceID {
		return errors.New("Worker tunnel identity mismatch")
	}
	if !life.ExpiresAt.After(time.Now().Add(3 * time.Minute)) {
		var renewed wire.ApplicationConnection
		if e = c.json(ctx, "POST", "/application/connection/renew", struct{}{}, &renewed, "", ""); e != nil {
			return e
		}
		if renewed.ConnectionId != life.ConnectionId || renewed.ApplicationId != life.ApplicationId || renewed.PrincipalId != life.PrincipalId || renewed.Revoked {
			return errors.New("Worker lease binding mismatch")
		}
		life = renewed
	}
	s.mu.Lock()
	previous := s.client
	s.client = c
	s.info = info
	s.state.StoreID = ep.StoreID
	s.state.InstanceID = ep.InstanceID
	s.state.PrincipalID = ep.PrincipalID
	s.state.Endpoint = ep.Origin
	s.state.Connection = life
	s.generation++
	if s.streamCancel != nil {
		s.streamCancel()
	}
	s.streamCtx = nil
	s.streamCancel = nil
	s.streams = map[string]bool{}
	e = s.saveLocked()
	s.mu.Unlock()
	if previous != nil {
		previous.http.CloseIdleConnections()
	}
	ok = e == nil
	return e
}
func (w *WorkerClient) Connect(ctx context.Context) error {
	s := w.engine

	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	closed, connected := s.closed, s.connected
	s.mu.Unlock()
	if closed {
		return errors.New("Worker connection closed")
	}
	if connected {
		return nil
	}
	if s.loadErr != nil {
		return s.loadErr
	}
	if e := s.connectWorker(ctx); e != nil {
		return s.fail(e)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("Worker connection detached during enrollment")
	}
	s.connected = true
	s.issue = ""
	if s.cancel == nil {
		s.ctx, s.cancel = context.WithCancel(context.Background())
		s.wg.Add(1)
		go s.pollLoop(s.ctx)
	}
	s.bumpLocked()
	s.mu.Unlock()
	return nil
}
func (w *WorkerClient) Reconnect(ctx context.Context) error {
	w.engine.mu.Lock()
	w.engine.connected = false
	w.engine.mu.Unlock()
	return w.Connect(ctx)
}
func (w *WorkerClient) Close(ctx context.Context) error         { return w.engine.Close(ctx) }
func (w *WorkerClient) WorkAdmission(ctx context.Context) error { return w.engine.WorkAdmission(ctx) }
func (w *WorkerClient) WorkStates() []api.WorkState             { return w.engine.WorkStates() }
func (w *WorkerClient) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	if !validWorkerDigest(in.RequestDigest) {
		return api.Task{}, errors.New("Worker durable request digest required")
	}
	if in.Target == nil || *in.Target != w.engine.workerTarget {
		return api.Task{}, errors.New("Worker task target does not match its route")
	}
	return w.engine.StartWork(ctx, in)
}
func (w *WorkerClient) ReadWork(ctx context.Context, id string) (api.Task, error) {
	return w.engine.ReadWork(ctx, id)
}
func (w *WorkerClient) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	if !validWorkerDigest(in.RequestDigest) {
		return api.Task{}, errors.New("Worker durable continuation digest required")
	}
	return w.engine.SendWork(ctx, in)
}
func (w *WorkerClient) StopWork(ctx context.Context, id string) (api.Task, error) {
	return w.engine.StopWork(ctx, id)
}
func (w *WorkerClient) WorkMessageRecorded(in api.TaskMessage) bool {
	return w.engine.WorkMessageRecorded(in)
}
func (w *WorkerClient) Snapshot() api.Snapshot { return w.engine.Snapshot() }
func (w *WorkerClient) WaitSnapshot(ctx context.Context, rev uint64) (api.Snapshot, error) {
	return w.engine.WaitSnapshot(ctx, rev)
}
func (w *WorkerClient) Decide(ctx context.Context, d api.Decision) error {
	return w.engine.Decide(ctx, d)
}
func (w *WorkerClient) ResolveWorkWorkspace(ctx context.Context, id, requested string) (string, error) {
	if w.workspace == nil {
		return "", errors.New("Worker workspace unavailable")
	}
	return w.workspace.ResolveWorkWorkspace(ctx, id, requested)
}
func (w *WorkerClient) PrepareWorkWorkspace(ctx context.Context, id, workspace string, selected bool) error {
	if w.workspace == nil {
		return errors.New("Worker workspace unavailable")
	}
	return w.workspace.PrepareWorkWorkspace(ctx, id, workspace, selected)
}

var _ api.WorkRuntime = (*WorkerClient)(nil)

func validWorkerDigest(v string) bool {
	if len(v) != 64 {
		return false
	}
	raw, err := hex.DecodeString(v)
	return err == nil && len(raw) == 32
}

// ModelReadiness reports native public metadata, never live login permission.
// Transport setup/enrollment remains possible while model setup is incomplete.
func (w *WorkerClient) ModelReadiness() (bool, string) {
	s := w.engine
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workerModelConfigured, s.workerModelAuth
}

// Ordinary shared Host workers never attest an owned lease fence.
func (*WorkerClient) LeaseAwareAdmission() bool { return false }
