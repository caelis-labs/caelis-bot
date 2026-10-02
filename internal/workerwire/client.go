package workerwire

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type Client struct {
	stream  io.ReadWriteCloser
	pair    Pair
	source  api.WorkSourceProvider
	gate    chan struct{}
	mu      sync.Mutex
	pending map[uint64]chan frame
	state   State
	closed  chan struct{}
	once    sync.Once
	next    uint64
	life    context.Context
	cancel  context.CancelFunc
}

// NewClient owns only the authenticated native stream. Close/EOF never sends a
// Worker Stop or terminates the target's owner. Source comes from the actual
// resident native adapter, outside all model/renderer schemas.
func NewClient(ctx context.Context, pair Pair, source api.WorkSourceProvider, stream io.ReadWriteCloser) (*Client, error) {
	if !pair.valid() || source == nil || stream == nil {
		return nil, errors.New("invalid paired Worker client")
	}
	life, cancel := context.WithCancel(context.Background())
	c := &Client{stream: stream, pair: pair, source: source, gate: make(chan struct{}, 1), pending: map[uint64]chan frame{}, closed: make(chan struct{}), life: life, cancel: cancel}
	go c.read()
	if _, err := c.call(ctx, frame{Method: "hello"}); err != nil {
		c.Close()
		return nil, err
	}
	if !c.Ready() {
		c.Close()
		return nil, errors.New("paired native Worker owner is not ready")
	}
	go c.watch()
	return c, nil
}

func (c *Client) Close()                { c.once.Do(func() { c.cancel(); close(c.closed); _ = c.stream.Close() }) }
func (c *Client) Done() <-chan struct{} { return c.closed }
func (c *Client) Ready() bool {
	select {
	case <-c.closed:
		return false
	default:
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.Connection == "ready"
}
func (c *Client) read() {
	defer c.Close()
	var staged *State
	var stagedID uint64
	for {
		f, err := readFrame(c.stream)
		if err != nil || f.Pair != c.pair || f.Method != "" {
			return
		}
		if f.State != nil && !c.validState(*f.State) {
			return
		}
		if f.State != nil && f.State.Pages != 0 {
			part := f.State
			if part.Pages < 2 || part.Page < 1 || part.Page > part.Pages {
				return
			}
			if part.Page == 1 {
				if staged != nil {
					return
				}
				copy := *part
				staged = &copy
				stagedID = f.ID
			} else {
				if staged == nil || stagedID != f.ID || staged.Page+1 != part.Page || staged.Pages != part.Pages || staged.Revision != part.Revision || staged.Connection != part.Connection || staged.LeaseAware != part.LeaseAware {
					return
				}
				staged.Tasks = append(staged.Tasks, part.Tasks...)
				staged.Approvals = append(staged.Approvals, part.Approvals...)
				staged.Artifacts = append(staged.Artifacts, part.Artifacts...)
				staged.Page = part.Page
			}
			if part.Page < part.Pages {
				continue
			}
			staged.Page, staged.Pages = 0, 0
			f.State = staged
			staged = nil
		} else if staged != nil || f.State != nil && f.State.Page != 0 {
			return
		}
		c.mu.Lock()
		if f.State != nil && f.State.Revision >= c.state.Revision {
			c.state = *f.State
		}
		reply := c.pending[f.ID]
		c.mu.Unlock()
		if reply != nil {
			select {
			case reply <- f:
			default:
			}
		}
	}
}
func (c *Client) validState(state State) bool {
	if len(state.Tasks) > 1024 || len(state.Approvals) > 1024 || len(state.Artifacts) > 8192 {
		return false
	}
	for _, v := range state.Tasks {
		if v.Target != c.pair.Target || v.Task.Target == nil || *v.Task.Target != c.pair.Target {
			return false
		}
	}
	for _, v := range state.Approvals {
		if v.Target != c.pair.Target {
			return false
		}
	}
	for _, v := range state.Artifacts {
		if v.Target != c.pair.Target {
			return false
		}
	}
	return true
}
func (c *Client) watch() {
	for {
		c.mu.Lock()
		revision := c.state.Revision
		c.mu.Unlock()
		if _, err := c.call(c.life, frame{Method: "watch", Revision: revision}); err != nil {
			return
		}
	}
}

func (c *Client) call(ctx context.Context, f frame) (frame, error) {
	return c.callAdmitted(ctx, f, nil)
}

func (c *Client) callAdmitted(ctx context.Context, f frame, admit func() error) (frame, error) {
	if err := ctx.Err(); err != nil {
		return frame{}, err
	}
	f.Version = 1
	f.Pair = c.pair
	expectedTaskID := f.TaskID
	if f.Start != nil {
		expectedTaskID = f.Start.ID
	}
	if f.Message != nil {
		expectedTaskID = f.Message.ID
	}
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return frame{}, ctx.Err()
	case <-c.closed:
		return frame{}, errors.New("Worker stream disconnected")
	}
	if err := ctx.Err(); err != nil {
		<-c.gate
		return frame{}, err
	}
	if admit != nil {
		if err := admit(); err != nil {
			<-c.gate
			return frame{}, err
		}
	}
	c.next++
	if c.next == 0 {
		<-c.gate
		c.Close()
		return frame{}, errors.New("Worker request sequence exhausted")
	}
	f.ID = c.next
	reply := make(chan frame, 1)
	c.mu.Lock()
	c.pending[f.ID] = reply
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, f.ID); c.mu.Unlock() }()
	stopWrite := context.AfterFunc(ctx, c.Close)
	err := writeFrame(c.stream, f)
	stopWrite()
	<-c.gate
	if err != nil {
		c.Close()
		return frame{}, errors.New("Worker stream write outcome unconfirmed")
	}
	select {
	case f := <-reply:
		if f.Task != nil && f.Task.ID != "" {
			if f.Task.ID != expectedTaskID || f.Task.Target == nil || *f.Task.Target != c.pair.Target {
				c.Close()
				return frame{}, errors.New("Worker original task receipt mismatch")
			}
		}
		if f.Fault == "state-record-too-large" || f.Fault == "state-envelope-too-large" {
			return f, errors.New("Worker state exceeds the encoded frame limit; original operation outcome remains unconfirmed")
		}
		if f.Fault != "" {
			return f, errors.New("Worker operation unavailable or unconfirmed")
		}
		return f, nil
	case <-ctx.Done():
		return frame{}, ctx.Err()
	case <-c.closed:
		return frame{}, errors.New("Worker stream disconnected; original intent retained")
	}
}

func (c *Client) nativeSource(ctx context.Context, original *api.WorkDispatchSource) (api.WorkDispatchSource, error) {
	if err := ctx.Err(); err != nil {
		return api.WorkDispatchSource{}, err
	}
	actual, err := c.source.WorkDispatchSource(ctx)
	if err != nil {
		return actual, err
	}
	if actual.Validate() != nil || actual.NodeID != c.pair.SourceNode || actual.Backend != c.pair.SourceBackend || original != nil && actual != *original {
		return api.WorkDispatchSource{}, errors.New("Worker source is not the current paired native invocation")
	}
	return actual, nil
}

func (c *Client) WorkAdmission(ctx context.Context) error {
	source, err := c.nativeSource(ctx, nil)
	if err != nil {
		return err
	}
	_, err = c.callAdmitted(ctx, frame{Method: "admission", Current: &source}, func() error { _, err := c.nativeSource(ctx, &source); return err })
	return err
}
func (c *Client) WorkStates() []api.WorkState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return clone(c.state.Tasks)
}
func (c *Client) WorkApprovals() []api.WorkApproval {
	c.mu.Lock()
	defer c.mu.Unlock()
	return clone(c.state.Approvals)
}
func (c *Client) WorkArtifacts() []api.WorkArtifactRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	return clone(c.state.Artifacts)
}
func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }

// Query first using the exact retained source/digest. A missing new intent has
// no authenticated current context, so it cannot dispatch. Only that explicit
// source-missing result permits a fresh native source check and first dispatch.
func (c *Client) mutation(ctx context.Context, f frame, original api.WorkDispatchSource) (api.Task, error) {
	f.Source = &original
	reply, err := c.call(ctx, f)
	if err != nil && reply.NeedsSource {
		source, e := c.nativeSource(ctx, &original)
		if e != nil {
			return taskFrom(reply), e
		}
		f.Current = &source
		reply, err = c.callAdmitted(ctx, f, func() error { _, err := c.nativeSource(ctx, &original); return err })
	}
	if reply.Task != nil && reply.Task.ID != "" && (reply.Task.Target == nil || *reply.Task.Target != c.pair.Target) {
		c.Close()
		return api.Task{}, errors.New("Worker receipt target mismatch")
	}
	return taskFrom(reply), err
}
func taskFrom(f frame) api.Task {
	if f.Task == nil {
		return api.Task{}
	}
	return clone(*f.Task)
}
func (c *Client) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	if in.Target == nil || *in.Target != c.pair.Target {
		return api.Task{}, errors.New("Worker target mismatch")
	}
	return c.mutation(ctx, frame{Method: "start", Start: &in, Digest: in.RequestDigest}, in.Source)
}
func (c *Client) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	return c.mutation(ctx, frame{Method: "send", Message: &in, Digest: in.RequestDigest}, in.Source)
}
func (c *Client) ReadWork(ctx context.Context, id string) (api.Task, error) {
	f, err := c.call(ctx, frame{Method: "read", TaskID: id})
	return taskFrom(f), err
}
func (c *Client) StopWork(ctx context.Context, id string) (api.Task, error) {
	source, err := c.controlSource(ctx)
	if err != nil {
		return api.Task{}, err
	}
	f, err := c.callAdmitted(ctx, frame{Method: "stop", TaskID: id, Current: source}, func() error { return c.checkControlSource(ctx, source) })
	return taskFrom(f), err
}
func (c *Client) WorkMessageRecorded(in api.TaskMessage) bool {
	ctx, cancel := context.WithTimeout(c.life, 3*time.Second)
	defer cancel()
	f, err := c.call(ctx, frame{Method: "recorded", Message: &in, Source: &in.Source, Digest: in.RequestDigest})
	return err == nil && f.Recorded
}
func (c *Client) ResolveWorkWorkspace(ctx context.Context, id, path string) (string, error) {
	f, err := c.call(ctx, frame{Method: "resolve", TaskID: id, Workspace: path})
	return f.Path, err
}
func (c *Client) PrepareWorkWorkspace(ctx context.Context, id, path string, selected bool) error {
	source, err := c.nativeSource(ctx, nil)
	if err != nil {
		return err
	}
	_, err = c.callAdmitted(ctx, frame{Method: "prepare", TaskID: id, Workspace: path, Selected: selected, Current: &source}, func() error { _, err := c.nativeSource(ctx, &source); return err })
	return err
}
func (c *Client) DecideWork(ctx context.Context, a api.WorkApproval, d api.Decision) error {
	if a.Target != c.pair.Target {
		return errors.New("Worker decision target mismatch")
	}
	source, err := c.controlSource(ctx)
	if err != nil {
		return err
	}
	_, err = c.callAdmitted(ctx, frame{Method: "decide", Approval: &a, Decision: &d, Current: source}, func() error { return c.checkControlSource(ctx, source) })
	return err
}
func (c *Client) ReadWorkArtifact(ctx context.Context, taskID, id string) (api.WorkArtifact, error) {
	f, err := c.call(ctx, frame{Method: "artifact", TaskID: taskID, ArtifactID: id})
	if err != nil {
		return api.WorkArtifact{}, err
	}
	if f.Artifact == nil || f.Artifact.ID != id || len(f.Data) > api.MaxWorkArtifactBytes || int64(len(f.Data)) != f.Artifact.Size {
		return api.WorkArtifact{}, errors.New("Worker artifact receipt mismatch")
	}
	digest := sha256.Sum256(f.Data)
	if hex.EncodeToString(digest[:]) != f.Artifact.SHA256 {
		return api.WorkArtifact{}, errors.New("Worker artifact digest mismatch")
	}
	v := *f.Artifact
	v.Bytes = f.Data
	return v, nil
}

var _ api.WorkRuntime = (*Client)(nil)
var _ api.WorkWorkspaceProvider = (*Client)(nil)
var _ api.WorkApprovalProvider = (*Client)(nil)
var _ api.WorkArtifactProvider = (*Client)(nil)
var _ api.WorkArtifactCatalog = (*Client)(nil)
var _ api.RecordedWorkMessage = (*Client)(nil)

// LeaseAwareAdmission reports the paired target owner’s negotiated native fence.
func (c *Client) LeaseAwareAdmission() bool {
	if !c.Ready() {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.LeaseAware
}

// Idle UI controls carry no invented activation. The authenticated target owns
// original task authority; if a current source exists it must remain exact.
func (c *Client) controlSource(ctx context.Context) (*api.WorkDispatchSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, err := c.source.WorkDispatchSource(ctx)
	if err != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if errors.Is(err, api.ErrWorkSourceInactive) {
			return nil, nil
		}
		return nil, err
	}
	if source.Validate() != nil || source.NodeID != c.pair.SourceNode || source.Backend != c.pair.SourceBackend {
		return nil, errors.New("Worker control source differs from paired native invocation")
	}
	return &source, nil
}
func (c *Client) checkControlSource(ctx context.Context, original *api.WorkDispatchSource) error {
	actual, err := c.controlSource(ctx)
	if err != nil {
		return err
	}
	if original == nil && actual == nil {
		return nil
	}
	if original == nil || actual == nil || *actual != *original {
		return errors.New("Worker control source changed while queued")
	}
	return nil
}
