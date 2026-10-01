package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

// WorkerOptions is trusted target-side assembly. Execution is explicitly
// selected for this node or read from this node's native config, never inherited
// from a client machine's resident Bot profile. Source is the authenticated host
// invocation's native attestation; it is not a field a model can supply.
type WorkerOptions struct {
	Target                              api.WorkTarget
	Directory, WorkRoot, Binary, Socket string
	Execution                           api.WorkExecutionSettings
	Source                              api.WorkSourceProvider
	Pair                                *workerwire.Pair // Trusted originating Bot pairing for a foreign-source owner.
	Lease                               *WorkerLeaseOptions
	RequireApproval                     bool // Tighten policy for isolated native acceptance fixtures.
}

// WorkerClient exposes no resident Submit/Interrupt or Bot lifecycle. The private
// Session value reuses native projections/receipts, with an empty resident ID.
// Its native server belongs to the persistent target owner, not an observer.
type WorkerClient struct {
	lease    *workerLeaseFence
	engine   *Session
	target   api.WorkTarget
	source   api.WorkSourceProvider
	pair     workerwire.Pair
	endpoint string
	stop     func()
	owned    *Client
	open     func(context.Context, Options) (*Client, func(), string, error)
}

func NewWorker(opts WorkerOptions) *WorkerClient {
	root := opts.WorkRoot
	if root == "" {
		root = filepath.Join(opts.Directory, "Tasks")
	}
	s := NewSession(SessionOptions{Directory: opts.Directory, StateFile: filepath.Join(opts.Directory, "worker-bindings.json"), WorkRoot: root, Binary: opts.Binary, Socket: opts.Socket, WorkExecution: opts.Execution, RequireApproval: opts.RequireApproval})
	w := &WorkerClient{engine: s, target: opts.Target, source: opts.Source, open: openWorkerClient}
	if opts.Lease != nil {
		w.lease = newWorkerLeaseFence(w, *opts.Lease)
		w.open = func(ctx context.Context, native Options) (*Client, func(), string, error) {
			return openSupervisedWorkerClient(ctx, native, opts.Lease.HelperPath)
		}
		s.opts.Admission = w.lease
		if opts.Socket != "" || !w.lease.valid() {
			s.loadErr = errors.New("leased Worker requires pinned broker, power fence and isolated native process")
		}
	}
	if opts.Pair != nil {
		w.pair = *opts.Pair
		if opts.Lease != nil && (api.ProfileBotID(opts.Lease.RawBotID) != w.pair.BotID || opts.Lease.SourceNode != w.pair.SourceNode || opts.Lease.SourceBackend != w.pair.SourceBackend) {
			s.loadErr = errors.New("Worker lease pin differs from native origin pairing")
		}
		if w.pair.Target != opts.Target {
			s.loadErr = errors.New("Worker native pairing target mismatch")
		}
	}
	if opts.Target.Validate() != nil || opts.Target.Backend != "codex" || opts.Target.Role != api.RoleWorker || !filepath.IsAbs(opts.Directory) || !filepath.IsAbs(root) || opts.Source == nil {
		s.loadErr = errors.New("Codex Worker requires an explicit target, private directory and native source provider")
	}
	if s.binding.ThreadID != "" || s.binding.Pending != nil || s.binding.DelegationText != "" || len(s.binding.PastThreads) > 0 || len(s.binding.Scheduled) > 0 || len(s.binding.Dreams) > 0 {
		s.loadErr = errors.New("resident Bot bindings cannot be opened as a Worker owner")
	}
	for id, task := range s.binding.Tasks {
		if task == nil || task.View.ID != id || !validWorkID(id) || !taskRequestValid(task.WorkerStartID, task.OriginalPrompt) || task.View.Target == nil || *task.View.Target != opts.Target || !workerDigest(task.WorkerStartDigest) || task.WorkerSource == nil || task.WorkerSource.Validate() != nil || task.WorkerBinding != workerBinding(task) {
			s.loadErr = errors.New("Worker original target/source binding is unavailable")
			continue
		}
		for request, receipt := range task.Requests {
			if request == "" || !workerDigest(receipt.RequestDigest) || receipt.Source == nil || receipt.Source.Validate() != nil {
				s.loadErr = errors.New("Worker original continuation binding is unavailable")
			}
		}
	}
	return w
}

// Keep process ownership outside transport.fail: a lost native observation
// socket must not reap the target's server. Only explicit owner shutdown stops it.
func openWorkerClient(ctx context.Context, opts Options) (*Client, func(), string, error) {
	if opts.Socket != "" {
		conn, err := connectExisting(ctx, opts.Socket)
		if err != nil {
			return nil, nil, "", err
		}
		client, err := initializeClient(ctx, conn, nil, opts)
		return client, nil, opts.Socket, err
	}
	opts.Attachable = true
	conn, stop, err := startProcess(ctx, opts)
	if err != nil {
		return nil, nil, "", err
	}
	endpoint, ok := conn.(interface{ terminalEndpoint() string })
	if !ok || endpoint.terminalEndpoint() == "" {
		conn.Close()
		stop()
		return nil, nil, "", errors.New("Worker owner requires a private native endpoint")
	}
	client, err := initializeClient(ctx, conn, nil, opts)
	if err != nil {
		stop()
		return nil, nil, "", err
	}
	return client, stop, endpoint.terminalEndpoint(), nil
}

// Connect bounds only startup. Native process and observation lifetime are
// owned by this target object and survive caller/observer context cancellation.
func (w *WorkerClient) Connect(ctx context.Context) error {
	s := w.engine
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := s.operation(ctx, 30*time.Second)
	defer cancel()
	s.mu.Lock()
	if s.loadErr != nil {
		err := s.loadErr
		s.mu.Unlock()
		return err
	}
	if s.closed || s.closing {
		s.mu.Unlock()
		return errors.New("Worker owner stopped")
	}
	if s.client != nil && s.client.Err() == nil && s.state.Connection == "ready" {
		s.mu.Unlock()
		return nil
	}
	old := s.client
	s.client = nil
	s.epoch++
	epoch := s.epoch
	s.loading = true
	s.buffer = nil
	s.state.Connection = "connecting"
	s.update()
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	for _, directory := range []string{s.opts.Directory, s.opts.WorkRoot} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return s.connectionError("Worker directory unavailable", err)
		}
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return s.connectionError("Worker directory redirected", ErrProtocol)
		}
	}
	root, err := api.ResolveTaskWorkspace(s.opts.WorkRoot)
	if err != nil {
		return s.connectionError("Worker workspace root unavailable", err)
	}
	directory, err := filepath.EvalSymlinks(s.opts.Directory)
	if err != nil {
		return s.connectionError("Worker directory unavailable", err)
	}
	s.mu.Lock()
	s.opts.WorkRoot = root
	s.opts.Directory = directory
	s.opts.StateFile = filepath.Join(directory, "worker-bindings.json")
	retainedTasks := len(s.binding.Tasks) > 0
	s.mu.Unlock()
	if w.pair.BotID != "" {
		if err = workerwire.BindPair(directory, w.pair, retainedTasks); err != nil {
			return s.connectionError("Worker original origin pairing unavailable", err)
		}
	}
	socket := s.opts.Socket
	if w.endpoint != "" {
		socket = w.endpoint
	}
	c, stop, endpoint, err := w.open(ctx, Options{Binary: s.opts.Binary, Socket: socket, Directory: s.opts.Directory, Experimental: true, HandleRequests: true, Attachable: true})
	if err != nil {
		return s.connectionError("Worker native connection unavailable; original bindings retained", err)
	}
	if stop != nil {
		if w.stop != nil {
			c.Close()
			stop()
			return errors.New("Worker cannot replace its original native process owner")
		}
		w.stop = stop
		s.mu.Lock()
		w.owned = c
		s.mu.Unlock()
	}
	w.endpoint = strings.TrimPrefix(endpoint, "unix://")
	s.mu.Lock()
	s.client = c
	s.mu.Unlock()
	auth, err := c.ReadAuthStatus(ctx)
	if err != nil || auth.RequiresOpenAIAuth && !auth.AccountPresent {
		c.Close()
		if err == nil {
			err = errors.New("target Codex login unavailable")
		}
		return s.connectionError("Worker target authentication unavailable", err)
	}
	s.mu.Lock()
	s.resetProjection()
	s.client = c
	s.bound = true
	s.state.Connection = "ready"
	s.state.Phase = "idle"
	s.state.Message = ""
	s.loading = false
	s.buffer = nil
	s.update()
	var ids []string
	for _, task := range s.binding.Tasks {
		if task.Thread != "" {
			ids = append(ids, task.Thread)
			if !terminal(task.View.Status) {
				s.childRuns[task.Thread] = task.Run
			}
			s.childWatching[task.Thread] = true
		}
	}
	s.mu.Unlock()
	if w.lease != nil {
		if err = w.lease.activate(ctx); err != nil {
			return err
		}
	}
	go s.listen(c, epoch)
	// Restore only retained native bindings; never create a replacement thread.
	for _, id := range ids {
		s.watchChild(c, epoch, id)
	}
	return nil
}

func (w *WorkerClient) WorkerPair() workerwire.Pair { return w.pair }

func (w *WorkerClient) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s := w.engine
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.closing || s.client == nil || s.client.Err() != nil || s.state.Connection != "ready" {
		return errors.New("Worker target is not connected")
	}
	return nil
}

func (w *WorkerClient) authorize(ctx context.Context, source api.WorkDispatchSource) error {
	if err := w.ready(ctx); err != nil {
		return err
	}
	if err := source.Validate(); err != nil {
		return err
	}
	actual, err := w.source.WorkDispatchSource(ctx)
	if err != nil {
		return err
	}
	if actual.Validate() != nil || actual != source {
		return errors.New("Worker source does not match the current native invocation")
	}
	return w.checkWorkerLease(ctx, source)
}

func (w *WorkerClient) WorkAdmission(ctx context.Context) error {
	if err := w.ready(ctx); err != nil {
		return err
	}
	source, err := w.source.WorkDispatchSource(ctx)
	if err != nil {
		return err
	}
	if err = source.Validate(); err != nil {
		return err
	}
	return w.checkWorkerLease(ctx, source)
}

func workerDigest(v string) bool {
	b, err := hex.DecodeString(v)
	return err == nil && len(v) == 64 && len(b) == 32
}

// Detect retained intent/native identity drift before restoring any subscription.
// This is journal integrity, not authority from a hash; authority remains the
// authenticated source provider and the private target owner's original receipt.
func workerBinding(task *taskRecord) string {
	value := struct {
		ID, Title, Workspace, Prompt, Instructions, StartID, Digest, Thread string
		Target                                                              *api.WorkTarget
		Source                                                              *api.WorkDispatchSource
	}{task.View.ID, task.View.Title, task.View.Workspace, task.OriginalPrompt, task.Instructions, task.WorkerStartID, task.WorkerStartDigest, task.Thread, task.View.Target, task.WorkerSource}
	bytes, _ := json.Marshal(value)
	digest := sha256.Sum256(bytes)
	return hex.EncodeToString(digest[:])
}
func copyWorkerTask(task api.Task) api.Task {
	if task.Target != nil {
		target := *task.Target
		task.Target = &target
	}
	return task
}

func (w *WorkerClient) WorkStates() []api.WorkState {
	states := w.engine.WorkStates()
	for i := range states {
		states[i].Target = w.target
		states[i].Task = copyWorkerTask(states[i].Task)
	}
	return states
}

func (w *WorkerClient) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	if !taskRequestValid(in.RequestID, in.Prompt) || !validWorkID(in.ID) || strings.TrimSpace(in.Title) == "" || len(in.Title) > 160 || in.Target == nil || *in.Target != w.target || !workerDigest(in.RequestDigest) || !filepath.IsAbs(in.Workspace) || strings.TrimSpace(in.Instructions) == "" {
		return api.Task{}, errors.New("Worker requires stable task/target/source intent")
	}
	s := w.engine
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := s.operation(ctx, 8*time.Second)
	defer cancel()
	fingerprint := opaque(in.Title, in.Prompt)
	if in.TaskStart.Workspace != "" {
		fingerprint = opaque(in.Title, in.Prompt, in.TaskStart.Workspace)
	}
	s.mu.Lock()
	if previous := s.binding.Tasks[in.ID]; previous != nil {
		view := copyWorkerTask(s.taskView(previous))
		same := previous.WorkerStartID == in.RequestID && previous.WorkerStartDigest == in.RequestDigest && previous.WorkerSource != nil && *previous.WorkerSource == in.Source && previous.Fingerprint == fingerprint && previous.View.Workspace == in.Workspace && previous.Instructions == in.Instructions
		s.mu.Unlock()
		if !same {
			return view, errors.New("Worker start intent conflicts with its original receipt")
		}
		return view, nil
	}
	s.mu.Unlock()
	if err := w.authorize(ctx, in.Source); err != nil {
		return api.Task{}, err
	}
	if err := validateWorkWorkspace(s.workRoot(), in.Workspace, in.TaskStart.Workspace != ""); err != nil {
		return api.Task{}, err
	}
	execution, err := s.resolveWorkExecution(ctx, in.Workspace)
	if err != nil {
		return api.Task{}, err
	}
	if err = w.authorize(ctx, in.Source); err != nil {
		return api.Task{}, err
	}
	target, source := w.target, in.Source
	task := &taskRecord{View: api.Task{ID: in.ID, Target: &target, Title: in.Title, Workspace: in.Workspace, Status: "unknown", Outcome: "unknown"}, Fingerprint: fingerprint, OriginalPrompt: in.Prompt, WorkerStartID: in.RequestID, WorkerStartDigest: in.RequestDigest, WorkerSource: &source, Requests: map[string]taskReceipt{}, Instructions: in.Instructions, Execution: &execution}
	task.WorkerBinding = workerBinding(task)
	s.mu.Lock()
	if s.binding.Tasks == nil {
		s.binding.Tasks = map[string]*taskRecord{}
	}
	s.binding.Tasks[in.ID] = task
	if err = s.save(); err != nil {
		delete(s.binding.Tasks, in.ID)
		s.mu.Unlock()
		return api.Task{}, err
	}
	c := s.client
	s.mu.Unlock()
	var response threadExecutionResponse
	err = callDecode(ctx, c, "thread/start", s.workerParams(in.Workspace, in.Instructions, task), &response)
	s.mu.Lock()
	if err != nil || response.Thread.ID == "" || response.Model == "" || s.ownsThread(response.Thread.ID) {
		if err == nil {
			err = ErrProtocol
		}
		if definiteTaskRejection(err) {
			task.View.Status = "failed"
			task.View.Outcome = "rejected"
		}
		saveErr := s.save()
		view := copyWorkerTask(task.View)
		s.mu.Unlock()
		return view, errors.Join(err, saveErr)
	}
	task.Thread = response.Thread.ID
	task.Execution = response.execution()
	task.ModelProvider = response.ModelProvider
	task.WorkerBinding = workerBinding(task)
	s.children[task.Thread] = true
	s.childWatching[task.Thread] = true
	err = s.save()
	view := copyWorkerTask(task.View)
	s.mu.Unlock()
	if err != nil {
		return view, err
	}
	view, err = s.sendTaskWithAdmission(ctx, task, api.TaskMessage{ID: in.ID, RequestID: in.RequestID, Prompt: in.Prompt, Source: in.Source, RequestDigest: in.RequestDigest}, false, func() error { return w.authorize(ctx, in.Source) })
	return copyWorkerTask(view), err
}

func (w *WorkerClient) ReadWork(ctx context.Context, id string) (api.Task, error) {
	view, err := w.engine.ReadWork(ctx, id)
	w.engine.mu.Lock()
	w.engine.update()
	w.engine.mu.Unlock()
	return copyWorkerTask(view), err
}
func (w *WorkerClient) WorkMessageRecorded(in api.TaskMessage) bool {
	s := w.engine
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.binding.Tasks[in.ID]
	if task == nil {
		return false
	}
	receipt, ok := task.Requests[in.RequestID]
	return ok && receipt.Fingerprint == opaque(in.Prompt) && receipt.RequestDigest == in.RequestDigest && receipt.Source != nil && *receipt.Source == in.Source
}

func (w *WorkerClient) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	if !taskRequestValid(in.RequestID, in.Prompt) || !workerDigest(in.RequestDigest) {
		return api.Task{}, errors.New("Worker continuation requires stable request intent")
	}
	s := w.engine
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := s.operation(ctx, 8*time.Second)
	defer cancel()
	s.mu.Lock()
	task := s.binding.Tasks[in.ID]
	if task == nil {
		s.mu.Unlock()
		return api.Task{}, errors.New("Worker task is not owned by this target")
	}
	if receipt, ok := task.Requests[in.RequestID]; ok {
		view := copyWorkerTask(s.taskView(task))
		view.Outcome = receipt.Outcome
		same := receipt.Fingerprint == opaque(in.Prompt) && receipt.RequestDigest == in.RequestDigest && receipt.Source != nil && *receipt.Source == in.Source
		s.mu.Unlock()
		if !same {
			return view, errors.New("Worker continuation conflicts with its original receipt")
		}
		return view, nil
	}
	if task.Thread == "" || task.Pending != "" || task.View.Status == "unknown" || len(task.Requests) >= 100 {
		view := copyWorkerTask(s.taskView(task))
		s.mu.Unlock()
		return view, errors.New("Worker original result must be reconciled before new work")
	}
	s.mu.Unlock()
	if err := w.authorize(ctx, in.Source); err != nil {
		return api.Task{}, err
	}
	view, err := s.sendTaskWithAdmission(ctx, task, in, true, func() error { return w.authorize(ctx, in.Source) })
	return copyWorkerTask(view), err
}

// Stop targets a retained native active turn. It does not require a resident
// model activation and never stops the server or another Worker's turn.
func (w *WorkerClient) StopWork(ctx context.Context, id string) (api.Task, error) {
	ticket, release, err := w.beginControl(ctx, id)
	if err != nil {
		view, readErr := w.ReadWork(ctx, id)
		return view, errors.Join(err, readErr)
	}
	defer release()
	ctx = ticket
	s := w.engine
	s.op.Lock()
	defer s.op.Unlock()
	if err := w.ready(ctx); err != nil {
		return api.Task{}, err
	}
	s.mu.Lock()
	task := s.binding.Tasks[id]
	if task == nil {
		s.mu.Unlock()
		return api.Task{}, errors.New("Worker task is not owned by this target")
	}
	run, c := s.childRuns[task.Thread], s.client
	view := copyWorkerTask(s.taskView(task))
	if w.lease != nil && task.WorkerStop != nil && task.WorkerStop.Outcome == "unknown" {
		s.mu.Unlock()
		return view, errors.New("original Worker cancellation remains unconfirmed")
	}
	if run == "" {
		s.mu.Unlock()
		return view, errors.New("Worker active native turn is unconfirmed")
	}
	previousStop, previousReceipt := task.SuppressReport, task.WorkerStop
	task.SuppressReport = true
	if w.lease != nil {
		task.WorkerStop = &workerStopReceipt{Thread: task.Thread, Run: run, Outcome: "unknown"}
	}
	err = s.save()
	if err != nil {
		task.SuppressReport = previousStop
		task.WorkerStop = previousReceipt
	}
	thread := task.Thread
	s.mu.Unlock()
	if err != nil {
		return view, err
	}
	ctx, cancel := s.operation(ctx, 8*time.Second)
	defer cancel()
	cancelErr := s.cancelElicitations(ctx, c, thread, run)
	err = errors.Join(cancelErr, callDecode(ctx, c, "turn/interrupt", map[string]string{"threadId": thread, "turnId": run}, nil))
	s.mu.Lock()
	if w.lease != nil && err == nil {
		task.WorkerStop.Outcome = "accepted"
		err = s.save()
	}
	view = copyWorkerTask(s.taskView(task))
	s.mu.Unlock()
	return view, err
}

func (w *WorkerClient) ResolveWorkWorkspace(ctx context.Context, id, requested string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !validWorkID(id) {
		return "", errors.New("Worker task identity is invalid")
	}
	if requested != "" {
		return api.ResolveTaskWorkspace(requested)
	}
	return filepath.Join(w.workRoot(), id), nil
}
func (w *WorkerClient) workRoot() string {
	w.engine.mu.Lock()
	defer w.engine.mu.Unlock()
	return w.engine.workRoot()
}
func (w *WorkerClient) PrepareWorkWorkspace(ctx context.Context, id, workspace string, selected bool) error {
	if w.lease != nil {
		if err := w.WorkAdmission(ctx); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validWorkID(id) {
		return errors.New("Worker task identity is invalid")
	}
	if selected {
		resolved, err := api.ResolveTaskWorkspace(workspace)
		if err != nil {
			return err
		}
		if resolved != workspace {
			return errors.New("Worker workspace redirected")
		}
		return nil
	}
	root := w.workRoot()
	if workspace != filepath.Join(root, id) {
		return errors.New("Worker workspace is outside its target root")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Worker workspace root redirected")
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err = directory.Mkdir(id, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return validateWorkWorkspace(root, workspace, false)
}

func (w *WorkerClient) WorkApprovals() []api.WorkApproval {
	s := w.engine
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []api.WorkApproval
	for _, prompt := range s.prompts {
		task := s.taskByThread(prompt.thread)
		if task == nil || prompt.view.Status != "pending" {
			continue
		}
		b, _ := json.Marshal(prompt.view)
		var approval api.Approval
		_ = json.Unmarshal(b, &approval)
		out = append(out, api.WorkApproval{TaskID: task.View.ID, Target: w.target, Approval: approval})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Approval.ID < out[j].Approval.ID })
	return out
}
func (w *WorkerClient) DecideWork(ctx context.Context, approval api.WorkApproval, decision api.Decision) error {
	ticket, release, err := w.beginControl(ctx, approval.TaskID)
	if err != nil {
		return err
	}
	defer release()
	ctx = ticket
	s := w.engine
	s.mu.Lock()
	task, prompt := s.binding.Tasks[approval.TaskID], s.prompts[approval.Approval.ID]
	valid := approval.Target == w.target && decision.ID == approval.Approval.ID && task != nil && prompt != nil && prompt.thread == task.Thread
	s.mu.Unlock()
	if !valid {
		return errors.New("Worker approval target does not match its owned task")
	}
	return s.Decide(ctx, decision)
}

func (w *WorkerClient) WaitSnapshot(ctx context.Context, revision uint64) (api.Snapshot, error) {
	return w.engine.WaitSnapshot(ctx, revision)
}
func (w *WorkerClient) Snapshot() api.Snapshot { return w.engine.Snapshot() }

// Close is explicit target-owner shutdown. Observer detachment must never call
// it; the retained process stop runs even if the native observation socket died.
func (w *WorkerClient) Close(ctx context.Context) error {
	if w.lease != nil {
		w.lease.revoke()
		w.lease.releasePower()
	}
	s := w.engine
	// The leased watchdog has already stopped and verified the exact owned
	// process tree. Asking that dead native connection to list/clean terminals
	// would turn confirmed native cleanup into a spurious unknown stop. Retain
	// every task receipt and use the independent process proof for this close.
	if w.lease != nil && w.owned != nil && w.lease.stopErr == nil && w.owned.toolCleanupError() == nil {
		// Drain admitted journal writers after the fence cancels native work.
		s.op.Lock()
		s.mu.Lock()
		s.closed = true
		s.state.Connection = "stopped"
		s.update()
		s.mu.Unlock()
		s.op.Unlock()
	}
	err := s.Close(ctx)
	s.op.Lock()
	defer s.op.Unlock()
	if w.stop != nil {
		w.stop()
		w.stop = nil
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client != nil {
		err = errors.Join(err, client.toolCleanupError())
	}
	if w.owned != nil {
		err = errors.Join(err, w.owned.toolCleanupError())
	}
	if w.lease != nil {
		err = errors.Join(err, w.lease.stopErr)
	}
	return err
}

var _ api.WorkRuntime = (*WorkerClient)(nil)
var _ api.WorkWorkspaceProvider = (*WorkerClient)(nil)
var _ api.WorkApprovalProvider = (*WorkerClient)(nil)
var _ api.RecordedWorkMessage = (*WorkerClient)(nil)
