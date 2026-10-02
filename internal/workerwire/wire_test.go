package workerwire

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
)

func testPair() Pair {
	return Pair{Target: api.WorkTarget{NodeID: "node", Backend: "codex", Role: api.RoleWorker}, BotID: "product-bot", SourceNode: "host", SourceBackend: "codex"}
}
func TestClosedWorkerFramesRejectUnknownAndMixedCommands(t *testing.T) {
	valid := frame{Version: 1, ID: 1, Pair: testPair(), Method: "read", TaskID: "owned-task"}
	var buffer bytes.Buffer
	if err := writeFrame(&buffer, valid); err != nil {
		t.Fatal(err)
	}
	decoded, err := readFrame(&buffer)
	if err != nil || !validRequest(decoded) {
		t.Fatal(decoded, err)
	}
	decoded.Workspace = "/arbitrary"
	if validRequest(decoded) {
		t.Fatal("read accepted unrelated path authority")
	}
	decoded = valid
	decoded.Method = "shell"
	if validRequest(decoded) {
		t.Fatal("protocol widened to arbitrary method")
	}
	b, _ := json.Marshal(valid)
	b = append(b[:len(b)-1], []byte(`,"NativeThreadID":"foreign"}`)...)
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(b)))
	buffer.Reset()
	buffer.Write(prefix[:])
	buffer.Write(b)
	if _, err = readFrame(&buffer); err == nil {
		t.Fatal("unknown native target field accepted")
	}
	buffer.Reset()
	binary.BigEndian.PutUint32(prefix[:], maxFrame+1)
	buffer.Write(prefix[:])
	if _, err = readFrame(&buffer); err == nil {
		t.Fatal("unbounded frame accepted")
	}
	if _, err = SourceProvider().WorkDispatchSource(context.Background()); !errors.Is(err, errSourceMissing) {
		t.Fatal("ordinary context fabricated native authority", err)
	}
}

type partialStream struct {
	calls   atomic.Int32
	entered chan struct{}
	closed  chan struct{}
	once    sync.Once
}

type observedQueueContext struct {
	context.Context
	reached chan struct{}
	once    sync.Once
}

func (c *observedQueueContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.reached) })
	return c.Context.Done()
}

func (p *partialStream) Read([]byte) (int, error) { <-p.closed; return 0, io.EOF }
func (p *partialStream) Write(b []byte) (int, error) {
	if p.calls.Add(1) == 1 {
		return 1, nil
	}
	p.once.Do(func() { close(p.entered) })
	<-p.closed
	return 0, io.ErrClosedPipe
}
func (p *partialStream) Close() error {
	select {
	case <-p.closed:
	default:
		close(p.closed)
	}
	return nil
}

func TestQueuedCancellationPreservesAdmittedFrameAndPartialCancellationClosesStream(t *testing.T) {
	p := &partialStream{entered: make(chan struct{}), closed: make(chan struct{})}
	life, cancelLife := context.WithCancel(context.Background())
	defer cancelLife()
	c := &Client{stream: p, pair: testPair(), gate: make(chan struct{}, 1), pending: map[uint64]chan frame{}, closed: make(chan struct{}), life: life, cancel: cancelLife}
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { _, err := c.call(firstCtx, frame{Method: "state"}); firstDone <- err }()
	select {
	case <-p.entered:
	case <-time.After(time.Second):
		t.Fatal("partial prefix not admitted")
	}
	queuedBase, cancelQueued := context.WithCancel(context.Background())
	queuedCtx := &observedQueueContext{Context: queuedBase, reached: make(chan struct{})}
	queuedDone := make(chan error, 1)
	go func() { _, err := c.call(queuedCtx, frame{Method: "state"}); queuedDone <- err }()
	select {
	case <-queuedCtx.reached:
	case <-time.After(time.Second):
		t.Fatal("second frame did not queue")
	}
	cancelQueued()
	select {
	case err := <-queuedDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("queued cancellation failed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued cancellation blocked")
	}
	select {
	case <-p.closed:
		t.Fatal("queued request closed another admitted frame")
	default:
	}
	if p.calls.Load() != 2 {
		t.Fatal("queued cancellation wrote frame bytes", p.calls.Load())
	}
	cancelFirst()
	select {
	case err := <-firstDone:
		if err == nil {
			t.Fatal("partial prefix was reported accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not unblock admitted frame")
	}
	select {
	case <-c.Done():
	default:
		t.Fatal("partial frame continued on live stream")
	}
	if p.calls.Load() != 2 {
		t.Fatal("partial framing retried after cancellation")
	}
}

func TestWorkerSourceCannotEnterNestedModelStartJSON(t *testing.T) {
	start := api.WorkStart{Source: api.WorkDispatchSource{BindingID: "private"}, RequestDigest: "private"}
	b, err := json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("Source")) || bytes.Contains(b, []byte("RequestDigest")) || bytes.Contains(b, []byte("private")) {
		t.Fatal("host attestation entered model body")
	}
}

type mutableNativeSource struct {
	mu    sync.Mutex
	value api.WorkDispatchSource
}

func (s *mutableNativeSource) WorkDispatchSource(context.Context) (api.WorkDispatchSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, nil
}

func TestNativeSourceIsRecheckedAfterFrameAdmission(t *testing.T) {
	p := &partialStream{entered: make(chan struct{}), closed: make(chan struct{})}
	life, cancel := context.WithCancel(context.Background())
	defer cancel()
	original := api.WorkDispatchSource{NodeID: "host", Backend: "codex", BindingID: "thread", OperationID: "turn", Kind: "native_activation"}
	source := &mutableNativeSource{value: original}
	c := &Client{stream: p, pair: testPair(), source: source, gate: make(chan struct{}, 1), pending: map[uint64]chan frame{}, closed: make(chan struct{}), life: life, cancel: cancel}
	c.gate <- struct{}{}
	ctx := &observedQueueContext{Context: context.Background(), reached: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := c.callAdmitted(ctx, frame{Method: "admission", Current: &original}, func() error { _, err := c.nativeSource(ctx, &original); return err })
		done <- err
	}()
	select {
	case <-ctx.reached:
	case <-time.After(time.Second):
		t.Fatal("request did not reach write queue")
	}
	source.mu.Lock()
	source.value.OperationID = "later-turn"
	source.mu.Unlock()
	<-c.gate
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expired source admitted")
		}
	case <-time.After(time.Second):
		t.Fatal("native source check blocked")
	}
	if p.calls.Load() != 0 {
		t.Fatal("expired source emitted frame bytes")
	}
	select {
	case <-p.closed:
		t.Fatal("no-dispatch source rejection closed another observer")
	default:
	}
}

type lastingWorker struct {
	nodeworker.Client
	pair Pair
}

func (w lastingWorker) WorkerPair() Pair     { return w.pair }
func (lastingWorker) Snapshot() api.Snapshot { return api.Snapshot{Connection: "ready", Revision: 1} }
func (lastingWorker) WaitSnapshot(ctx context.Context, _ uint64) (api.Snapshot, error) {
	<-ctx.Done()
	return api.Snapshot{}, ctx.Err()
}
func (lastingWorker) WorkStates() []api.WorkState       { return nil }
func (lastingWorker) WorkApprovals() []api.WorkApproval { return nil }
func TestResidentStreamSurvivesMoreThan8192RequestsAndConcurrentWrites(t *testing.T) {
	pair := testPair()
	server, err := NewServer(nodeworker.New(lastingWorker{pair: pair}), pair)
	if err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	go func() { _ = server.Serve(t.Context(), right) }()
	c, err := NewClient(t.Context(), pair, SourceProvider(), left)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 8300; i++ {
		if _, err = c.call(t.Context(), frame{Method: "state"}); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Go(func() {
			if _, err := c.call(t.Context(), frame{Method: "state"}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

type historyWorker struct {
	lastingWorker
	states []api.WorkState
}

func (w historyWorker) WorkStates() []api.WorkState { return w.states }
func TestWorkerHistoryPagesReconnectBeyond1024Tasks(t *testing.T) {
	pair := testPair()
	for _, count := range []int{1024, 1025, 2050} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			runtime := historyWorker{lastingWorker: lastingWorker{pair: pair}}
			for i := 0; i < count; i++ {
				status := "completed"
				if i == count-1 {
					status = "working"
				}
				runtime.states = append(runtime.states, api.WorkState{Target: pair.Target, Task: api.Task{ID: fmt.Sprintf("task-%d", i), Target: &pair.Target, Status: status}})
			}
			owner := nodeworker.New(runtime)
			server, err := NewServer(owner, pair)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				left, right := net.Pipe()
				done := make(chan error, 1)
				go func() { done <- server.Serve(t.Context(), right) }()
				client, err := NewClient(t.Context(), pair, SourceProvider(), left)
				if err != nil {
					t.Fatal(err)
				}
				states := client.WorkStates()
				if len(states) != count || states[count-1].Task.Status != "working" {
					t.Fatal("snapshot lost active/history tasks", len(states))
				}
				client.Close()
				<-done
			}
			if len(runtime.states) != count {
				t.Fatal("projection removed journal history")
			}
		})
	}
}

func TestPagedStateNeverPublishesIncompleteOrMixedSnapshot(t *testing.T) {
	for _, broken := range []string{"disconnect", "revision", "sequence"} {
		t.Run(broken, func(t *testing.T) {
			left, right := net.Pipe()
			life, cancel := context.WithCancel(context.Background())
			c := &Client{stream: left, pair: testPair(), pending: map[uint64]chan frame{}, closed: make(chan struct{}), life: life, cancel: cancel, state: State{Revision: 1, Connection: "ready"}}
			defer c.Close()
			defer right.Close()
			go c.read()
			first := frame{Version: 1, ID: 1, Pair: testPair(), State: &State{Revision: 2, Connection: "ready", Page: 1, Pages: 2}}
			if err := writeFrame(right, first); err != nil {
				t.Fatal(err)
			}
			c.mu.Lock()
			revision := c.state.Revision
			c.mu.Unlock()
			if revision != 1 {
				t.Fatal("partial state was published")
			}
			if broken != "disconnect" {
				last := *first.State
				last.Page = 2
				if broken == "revision" {
					last.Revision = 3
				} else {
					last.Page = 1
				}
				first.State = &last
				if err := writeFrame(right, first); err != nil {
					t.Fatal(err)
				}
			}
			right.Close()
			select {
			case <-c.Done():
			case <-time.After(time.Second):
				t.Fatal("invalid stream remained open")
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.state.Revision != 1 {
				t.Fatal("incoherent snapshot replaced original state")
			}
		})
	}
}
