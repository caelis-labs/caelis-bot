// Package workerwire is a closed host-only Worker protocol over a paired native
// SSH stream/private Unix socket. It is not a model tool or renderer contract.
package workerwire

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
)

const maxFrame = 24 << 20

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var errSourceMissing = fmt.Errorf("authenticated foreign native source missing: %w", api.ErrWorkSourceInactive)

// Pair is trusted node/native-host configuration, not a claim accepted from a
// request. Same-user strict SSH and a private target socket authenticate it.
type Pair struct {
	Target                           api.WorkTarget
	BotID, SourceNode, SourceBackend string
}

func (p Pair) valid() bool {
	return p.Target.Validate() == nil && p.Target.Role == api.RoleWorker && identifier.MatchString(p.Target.NodeID) && identifier.MatchString(p.Target.Backend) && identifier.MatchString(p.BotID) && identifier.MatchString(p.SourceNode) && identifier.MatchString(p.SourceBackend)
}

type frame struct {
	Version                               int
	ID                                    uint64
	Pair                                  Pair
	Method                                string                  `json:",omitempty"`
	Source, Current                       *api.WorkDispatchSource `json:",omitempty"`
	Start                                 *api.WorkStart          `json:",omitempty"`
	Message                               *api.TaskMessage        `json:",omitempty"`
	TaskID, ArtifactID, Workspace, Digest string                  `json:",omitempty"`
	Selected                              bool                    `json:",omitempty"`
	Approval                              *api.WorkApproval       `json:",omitempty"`
	Decision                              *api.Decision           `json:",omitempty"`
	Revision                              uint64                  `json:",omitempty"`
	Fault                                 string                  `json:",omitempty"`
	NeedsSource                           bool                    `json:",omitempty"`
	Task                                  *api.Task               `json:",omitempty"`
	Path                                  string                  `json:",omitempty"`
	Recorded                              bool                    `json:",omitempty"`
	State                                 *State                  `json:",omitempty"`
	Artifact                              *api.WorkArtifact       `json:",omitempty"`
	Data                                  []byte                  `json:",omitempty"`
}

type State struct {
	Page       int  `json:",omitempty"`
	Pages      int  `json:",omitempty"`
	LeaseAware bool `json:",omitempty"`
	Revision   uint64
	Connection string
	Tasks      []api.WorkState
	Approvals  []api.WorkApproval
	Artifacts  []api.WorkArtifactRef
}

func writeFrame(w io.Writer, f frame) error {
	b, err := json.Marshal(f)
	if err != nil || len(b) > maxFrame {
		return errors.New("Worker frame limit")
	}
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(b)))
	for _, part := range [][]byte{prefix[:], b} {
		for len(part) > 0 {
			n, err := w.Write(part)
			if err != nil {
				return err
			}
			if n <= 0 || n > len(part) {
				return io.ErrShortWrite
			}
			part = part[n:]
		}
	}
	return nil
}

// Send one coherent snapshot in pages bounded by both record count and the
// complete encoded frame, including reply payloads and base64 artifact data.
// Plan every page before sending any of them; an oversized record cannot expose
// a prefix of a snapshot or erase the complete native journal.
func writeStateFrames(w io.Writer, f frame) error {
	if f.State == nil {
		return writeFrame(w, f)
	}
	state := *f.State
	part := state
	part.Tasks = []api.WorkState{}
	part.Approvals = []api.WorkApproval{}
	part.Artifacts = []api.WorkArtifactRef{}
	// There cannot be more nonempty pages than records. Reserve the largest
	// possible page-number encoding while measuring the invariant envelope.
	bound := max(2, len(state.Tasks)+len(state.Approvals)+len(state.Artifacts))
	part.Page, part.Pages = bound, bound
	f.State = &part
	envelope, err := json.Marshal(f)
	if err != nil || len(envelope) > maxFrame {
		return writeFrame(w, frame{Version: f.Version, ID: f.ID, Pair: f.Pair, Fault: "state-envelope-too-large"})
	}
	budget := maxFrame - len(envelope)
	var pages []State
	for {
		available := budget
		tasks, err := statePageCount(state.Tasks, 1024, &available)
		if err != nil {
			return err
		}
		approvals, err := statePageCount(state.Approvals, 1024, &available)
		if err != nil {
			return err
		}
		artifacts, err := statePageCount(state.Artifacts, 8192, &available)
		if err != nil {
			return err
		}
		if tasks+approvals+artifacts == 0 && len(state.Tasks)+len(state.Approvals)+len(state.Artifacts) > 0 {
			return writeFrame(w, frame{Version: f.Version, ID: f.ID, Pair: f.Pair, Fault: "state-record-too-large"})
		}
		part.Tasks, state.Tasks = state.Tasks[:tasks], state.Tasks[tasks:]
		part.Approvals, state.Approvals = state.Approvals[:approvals], state.Approvals[approvals:]
		part.Artifacts, state.Artifacts = state.Artifacts[:artifacts], state.Artifacts[artifacts:]
		// The byte calculation uses [] rather than null for empty arrays.
		if part.Tasks == nil {
			part.Tasks = []api.WorkState{}
		}
		if part.Approvals == nil {
			part.Approvals = []api.WorkApproval{}
		}
		if part.Artifacts == nil {
			part.Artifacts = []api.WorkArtifactRef{}
		}
		pages = append(pages, part)
		if len(state.Tasks)+len(state.Approvals)+len(state.Artifacts) == 0 {
			break
		}
	}
	for i := range pages {
		pages[i].Page, pages[i].Pages = 0, 0
		if len(pages) > 1 {
			pages[i].Page, pages[i].Pages = i+1, len(pages)
		}
		f.State = &pages[i]
		if err := writeFrame(w, f); err != nil {
			return err
		}
	}
	return nil
}

func statePageCount[T any](records []T, limit int, available *int) (int, error) {
	count := 0
	for _, record := range records[:min(limit, len(records))] {
		encoded, err := json.Marshal(record)
		if err != nil {
			return 0, err
		}
		size := len(encoded)
		if count > 0 {
			size++
		} // Array separator.
		if size > *available {
			break
		}
		*available -= size
		count++
	}
	return count, nil
}

func readFrame(r io.Reader) (frame, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return frame{}, err
	}
	n := binary.BigEndian.Uint32(prefix[:])
	if n == 0 || n > maxFrame {
		return frame{}, errors.New("Worker frame limit")
	}
	b := make([]byte, int(n))
	if _, err := io.ReadFull(r, b); err != nil {
		return frame{}, err
	}
	var f frame
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&f) != nil || d.Decode(new(any)) != io.EOF || f.Version != 1 || f.ID == 0 || !f.Pair.valid() {
		return frame{}, errors.New("invalid Worker frame")
	}
	return f, nil
}

// A caller cannot obtain an authenticated source by placing it in a normal
// context. Only this paired protocol's dispatcher supplies the private key.
type sourceKey struct{}
type controlKey struct{}

// PairedControl returns only the native server-installed principal for exact
// existing-task controls. It does not create a WorkDispatchSource or activation.
func PairedControl(ctx context.Context) (Pair, bool) {
	pair, ok := ctx.Value(controlKey{}).(Pair)
	return pair, ok
}

type foreignSource struct{}

func SourceProvider() api.WorkSourceProvider { return foreignSource{} }
func (foreignSource) WorkDispatchSource(ctx context.Context) (api.WorkDispatchSource, error) {
	v, ok := ctx.Value(sourceKey{}).(api.WorkDispatchSource)
	if !ok || v.Validate() != nil {
		return api.WorkDispatchSource{}, errSourceMissing
	}
	return v, nil
}

type Server struct {
	owner *nodeworker.Owner
	pair  Pair
}

func NewServer(owner *nodeworker.Owner, pair Pair) (*Server, error) {
	if owner == nil || !pair.valid() {
		return nil, errors.New("invalid Worker owner pairing")
	}
	port, ok := owner.Runtime().(interface{ WorkerPair() Pair })
	if !ok || port.WorkerPair() != pair {
		return nil, errors.New("native Worker owner origin/target pairing mismatch")
	}
	return &Server{owner: owner, pair: pair}, nil
}
func (s *Server) state() State {
	o := s.owner.Observe(context.Background())
	defer o.Detach()
	v, _ := o.Snapshot()
	state := State{Revision: v.Revision, Connection: v.Connection, Tasks: s.owner.Runtime().WorkStates(), Approvals: s.owner.Approvals().WorkApprovals()}
	if catalog, ok := s.owner.Runtime().(api.WorkArtifactCatalog); ok {
		state.Artifacts = catalog.WorkArtifacts()
	}
	if port, ok := s.owner.Runtime().(api.LeaseAwareWorkRuntime); ok {
		state.LeaseAware = port.LeaseAwareAdmission()
	}
	return state
}

// Serve detaches on EOF/cancellation; the node service separately owns Stop.
func (s *Server) Serve(ctx context.Context, stream io.ReadWriteCloser) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer stream.Close()
	stop := context.AfterFunc(ctx, func() { _ = stream.Close() })
	defer stop()
	gate := make(chan struct{}, 1)
	slots := make(chan struct{}, 8)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	var lastID uint64
	for {
		request, err := readFrame(stream)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if request.Pair != s.pair || request.ID <= lastID || !validRequest(request) {
			return errors.New("Worker pairing/request mismatch")
		}
		lastID = request.ID
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		workers.Go(func() {
			defer func() { <-slots }()
			reply := s.dispatch(ctx, request)
			select {
			case gate <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-gate }()
			if ctx.Err() != nil {
				return
			}
			stopWrite := context.AfterFunc(ctx, func() { _ = stream.Close() })
			err := writeStateFrames(stream, reply)
			stopWrite()
			if err != nil {
				cancel()
			}
		})
	}
}

func (s *Server) dispatch(ctx context.Context, in frame) frame {
	out := frame{Version: 1, ID: in.ID, Pair: s.pair}
	runtime := s.owner.Runtime()
	var err error
	if in.Current != nil {
		if in.Current.Validate() != nil || in.Current.NodeID != s.pair.SourceNode || in.Current.Backend != s.pair.SourceBackend {
			out.Fault = "authority"
			return out
		}
		ctx = context.WithValue(ctx, sourceKey{}, *in.Current)
	}
	if in.Source != nil && (in.Source.Validate() != nil || in.Source.NodeID != s.pair.SourceNode || in.Source.Backend != s.pair.SourceBackend) {
		out.Fault = "authority"
		return out
	}
	switch in.Method {
	case "hello", "state":
	case "admission":
		err = runtime.WorkAdmission(ctx)
	case "watch":
		observer := s.owner.Observe(ctx)
		defer observer.Detach()
		_, err = observer.WaitSnapshot(in.Revision)
	case "resolve":
		out.Path, err = s.owner.Workspace().ResolveWorkWorkspace(ctx, in.TaskID, in.Workspace)
	case "prepare":
		_, err = SourceProvider().WorkDispatchSource(ctx)
		if err == nil {
			err = s.owner.Workspace().PrepareWorkWorkspace(ctx, in.TaskID, in.Workspace, in.Selected)
		}
	case "start":
		if in.Start == nil || in.Source == nil || in.Start.Target == nil || *in.Start.Target != s.pair.Target {
			err = errors.New("invalid start")
			break
		}
		value := *in.Start
		value.Source = *in.Source
		value.RequestDigest = in.Digest
		v, e := runtime.StartWork(ctx, value)
		out.Task = &v
		err = e
	case "send", "recorded":
		if in.Message == nil || in.Source == nil {
			err = errors.New("invalid message")
			break
		}
		value := *in.Message
		value.Source = *in.Source
		value.RequestDigest = in.Digest
		if in.Method == "recorded" {
			if port, ok := runtime.(api.RecordedWorkMessage); ok {
				out.Recorded = port.WorkMessageRecorded(value)
			}
		} else {
			v, e := runtime.SendWork(ctx, value)
			out.Task = &v
			err = e
		}
	case "read":
		v, e := runtime.ReadWork(ctx, in.TaskID)
		out.Task = &v
		err = e
	case "stop":
		ctx = context.WithValue(ctx, controlKey{}, s.pair)
		v, e := runtime.StopWork(ctx, in.TaskID)
		out.Task = &v
		err = e
	case "decide":
		ctx = context.WithValue(ctx, controlKey{}, s.pair)
		if in.Approval == nil || in.Decision == nil || in.Approval.Target != s.pair.Target {
			err = errors.New("invalid decision")
		} else {
			err = s.owner.Approvals().DecideWork(ctx, *in.Approval, *in.Decision)
		}
	case "artifact":
		if port, ok := runtime.(api.WorkArtifactProvider); ok {
			v, e := port.ReadWorkArtifact(ctx, in.TaskID, in.ArtifactID)
			err = e
			if e == nil && len(v.Bytes) <= api.MaxWorkArtifactBytes {
				out.Data = v.Bytes
				v.Bytes = nil
				out.Artifact = &v
			} else if e == nil {
				err = errors.New("artifact limit")
			}
		} else {
			err = errors.New("artifact unavailable")
		}
	default:
		err = errors.New("unsupported Worker command")
	}
	if err != nil {
		out.Fault = "operation"
		out.NeedsSource = errors.Is(err, errSourceMissing)
	}
	state := s.state()
	out.State = &state
	return out
}

func validRequest(in frame) bool {
	expected := frame{Version: 1, ID: in.ID, Pair: in.Pair, Method: in.Method}
	switch in.Method {
	case "hello", "state":
	case "admission":
		expected.Current = in.Current
	case "watch":
		expected.Revision = in.Revision
	case "resolve":
		expected.TaskID = in.TaskID
		expected.Workspace = in.Workspace
	case "prepare":
		expected.TaskID = in.TaskID
		expected.Workspace = in.Workspace
		expected.Selected = in.Selected
		expected.Current = in.Current
	case "start":
		expected.Start = in.Start
		expected.Source = in.Source
		expected.Current = in.Current
		expected.Digest = in.Digest
	case "send":
		expected.Message = in.Message
		expected.Source = in.Source
		expected.Current = in.Current
		expected.Digest = in.Digest
	case "recorded":
		expected.Message = in.Message
		expected.Source = in.Source
		expected.Digest = in.Digest
	case "read":
		expected.TaskID = in.TaskID
	case "stop":
		expected.TaskID = in.TaskID
		expected.Current = in.Current
	case "decide":
		expected.Current = in.Current
		expected.Approval = in.Approval
		expected.Decision = in.Decision
		expected.Current = in.Current
	case "artifact":
		expected.TaskID = in.TaskID
		expected.ArtifactID = in.ArtifactID
	default:
		return false
	}
	return reflect.DeepEqual(in, expected)
}
