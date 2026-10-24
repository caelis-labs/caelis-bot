package tasks

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type isolationReviewRuntime struct {
	*fixtureRuntime
	mu               sync.Mutex
	remoteRead       string
	entered, release chan struct{}
	readOnce         sync.Once
}

func (f *isolationReviewRuntime) WorkStates() []api.WorkState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fixtureRuntime.WorkStates()
}
func (f *isolationReviewRuntime) PrepareRemoteWork(context.Context, api.TaskStart, string) (string, error) {
	return "/isolated-review-workspace", nil
}
func (f *isolationReviewRuntime) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, e := f.fixtureRuntime.StartWork(ctx, in)
	v.Machine = in.Machine
	s := f.states[in.ID]
	s.Task = v
	f.states[in.ID] = s
	return v, e
}
func (f *isolationReviewRuntime) ReadWork(ctx context.Context, id string) (api.Task, error) {
	if id == f.remoteRead {
		f.readOnce.Do(func() { close(f.entered) })
		<-f.release
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fixtureRuntime.ReadWork(ctx, id)
}
func (f *isolationReviewRuntime) StopWork(ctx context.Context, id string) (api.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fixtureRuntime.StopWork(ctx, id)
}
func (f *isolationReviewRuntime) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fixtureRuntime.SendWork(ctx, in)
}

func isolationReviewManager(t *testing.T, f *isolationReviewRuntime) *Manager {
	t.Helper()
	root := t.TempDir()
	m, e := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "review-fixture", f, f.fixtureRuntime, f.Snapshot)
	if e != nil {
		t.Fatal(e)
	}
	return m
}

func TestReviewRemoteReadCannotBlockExistingLocalStop(t *testing.T) {
	f := &isolationReviewRuntime{fixtureRuntime: newRuntime(), entered: make(chan struct{}), release: make(chan struct{})}
	m := isolationReviewManager(t, f)
	local := start(t, m, "existing-local")
	in := input("remote-history")
	in.Machine = "peer-B"
	remote, e := m.StartTask(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	f.remoteRead = remote.ID
	readDone := make(chan struct{})
	go func() { defer close(readDone); _, _ = m.ReadTask(t.Context(), remote.ID) }()
	<-f.entered
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	stopDone := make(chan error, 1)
	go func() { _, e := m.StopTask(ctx, local.ID); stopDone <- e }()
	blocked := false
	select {
	case e := <-stopDone:
		if e != nil {
			t.Error(e)
		}
	case <-ctx.Done():
		blocked = true
	}
	close(f.release)
	<-readDone
	if blocked {
		<-stopDone
		t.Fatal("remote read owns global Manager lock; existing local Stop waits even past its context")
	}
}

func TestReviewUnknownRemoteCapacityCannotRejectLocalCreate(t *testing.T) {
	f := &isolationReviewRuntime{fixtureRuntime: newRuntime()}
	m := isolationReviewManager(t, f)
	m.ConfigureLimit(func() int { return 1 })
	f.unknownStart = true
	in := input("uncertain-remote")
	in.Machine = "peer-B"
	remote, e := m.StartTask(t.Context(), in)
	if e == nil || remote.Outcome != "unknown" {
		t.Fatal("setup must retain unknown remote execution", remote, e)
	}
	f.unknownStart = false
	local, e := m.StartTask(t.Context(), input("independent-local"))
	if e != nil {
		t.Fatalf("unknown remote execution exhausted local admission: %v", e)
	}
	if local.Machine != "" {
		t.Fatal("local request changed environment", local)
	}
	if r := m.state.Records[remote.ID]; r == nil || r.View.Status != "unknown" {
		t.Fatal(errors.New("remote unknown was cleared to admit local"))
	}
}

// Hold the native boundary, not the product API. Every independent operation
// below still passes through the same persisted Manager and runtime adapter.
type heldIsolationRuntime struct {
	*isolationReviewRuntime
	phase, task  string
	held, resume chan struct{}
	once         sync.Once
}

func (f *heldIsolationRuntime) hold(ctx context.Context, phase, id string) error {
	if f.phase != phase || f.task != id {
		return nil
	}
	f.once.Do(func() { close(f.held) })
	select {
	case <-f.resume:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (f *heldIsolationRuntime) PrepareRemoteWork(ctx context.Context, in api.TaskStart, id string) (string, error) {
	if e := f.hold(ctx, "prepare", in.RequestID); e != nil {
		return "", e
	}
	return f.isolationReviewRuntime.PrepareRemoteWork(ctx, in, id)
}
func (f *heldIsolationRuntime) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	if e := f.hold(ctx, "start", in.RequestID); e != nil {
		return api.Task{}, e
	}
	return f.isolationReviewRuntime.StartWork(ctx, in)
}
func (f *heldIsolationRuntime) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	if e := f.hold(ctx, "send", in.ID); e != nil {
		return api.Task{}, e
	}
	return f.isolationReviewRuntime.SendWork(ctx, in)
}
func (f *heldIsolationRuntime) ReadWorkConversation(ctx context.Context, id string) ([]api.TaskConversationMessage, error) {
	if e := f.hold(ctx, "history", id); e != nil {
		return nil, e
	}
	return []api.TaskConversationMessage{}, nil
}
func TestHeldPeerIOPreservesIndependentLocalAndPeerControls(t *testing.T) {
	for _, phase := range []string{"prepare", "start", "send", "history"} {
		t.Run(phase, func(t *testing.T) {
			f := &heldIsolationRuntime{isolationReviewRuntime: &isolationReviewRuntime{fixtureRuntime: newRuntime()}, held: make(chan struct{}), resume: make(chan struct{})}
			root := t.TempDir()
			m, e := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "fixture", f, f, f.Snapshot)
			if e != nil {
				t.Fatal(e)
			}
			local := start(t, m, "local-before-held-IO")
			b := input("B-before-held-IO")
			b.Machine = "peer-B"
			remote, e := m.StartTask(t.Context(), b)
			if e != nil {
				t.Fatal(e)
			}
			c := input("C-before-held-IO")
			c.Machine = "peer-C"
			other, e := m.StartTask(t.Context(), c)
			if e != nil {
				t.Fatal(e)
			}
			f.phase = phase
			f.task = remote.ID
			next := input("held-peer-IO")
			next.Machine = "peer-B"
			if phase == "start" || phase == "prepare" {
				f.task = next.RequestID
			}
			done := make(chan error, 1)
			go func() {
				var e error
				switch phase {
				case "prepare", "start":
					_, e = m.StartTask(t.Context(), next)
				case "send":
					_, e = m.SendTask(t.Context(), api.TaskMessage{ID: remote.ID, RequestID: "held-send", Prompt: "Continue fixture"})
				case "history":
					_, e = m.ReadTaskConversation(t.Context(), remote.ID)
				}
				done <- e
			}()
			select {
			case <-f.held:
			case e := <-done:
				t.Fatalf("native boundary was not reached: %v", e)
			case <-time.After(3 * time.Second):
				t.Fatal("native boundary did not enter")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
			defer cancel()
			independent := make(chan error, 1)
			go func() {
				if _, e := m.ReadTask(ctx, local.ID); e != nil {
					independent <- e
					return
				}
				if _, e := m.SendTask(ctx, api.TaskMessage{ID: local.ID, RequestID: "local-continue", Prompt: "Local continuation"}); e != nil {
					independent <- e
					return
				}
				if _, e := m.StopTask(ctx, local.ID); e != nil {
					independent <- e
					return
				}
				if _, e := m.ReadTask(ctx, other.ID); e != nil {
					independent <- e
					return
				}
				_, e := m.StartTask(ctx, input("local-during-held-IO"))
				independent <- e
			}()
			var independentErr error
			select {
			case independentErr = <-independent:
			case <-ctx.Done():
				independentErr = ctx.Err()
			}
			// An explicitly cancelled same-target waiter must not dispatch a second
			// native effect or retain an environment lock entry after completion.
			wait, cancelWait := context.WithCancel(t.Context())
			cancelWait()
			waitInput := input("cancelled-peer-waiter")
			waitInput.Machine = "peer-B"
			_, waitErr := m.StartTask(wait, waitInput)
			close(f.resume)
			if e := <-done; e != nil {
				t.Fatal(e)
			}
			if independentErr != nil {
				t.Fatalf("%s blocked unrelated local/peer controls: %v", phase, independentErr)
			}
			if !errors.Is(waitErr, context.Canceled) {
				t.Fatal("same-target cancellation lost", waitErr)
			}
			f.mu.Lock()
			stopped := f.states[local.ID].StopRequested
			f.mu.Unlock()
			if !stopped {
				t.Fatal("local native stop was not reached")
			}
			m.environmentMu.Lock()
			remaining := len(m.environmentOps)
			m.environmentMu.Unlock()
			if remaining != 0 {
				t.Fatal("completed/cancelled target locks retained", remaining)
			}
		})
	}
}
func TestPauseDuringPeerPreparationDoesNotDispatchOrLoseOtherOwners(t *testing.T) {
	f := &heldIsolationRuntime{isolationReviewRuntime: &isolationReviewRuntime{fixtureRuntime: newRuntime()}, phase: "prepare", task: "before-update", held: make(chan struct{}), resume: make(chan struct{})}
	root := t.TempDir()
	m, e := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "fixture", f, f, f.Snapshot)
	if e != nil {
		t.Fatal(e)
	}
	in := input("before-update")
	in.Machine = "peer-B"
	done := make(chan error, 1)
	go func() { _, e := m.StartTask(t.Context(), in); done <- e }()
	<-f.held
	if e := m.PauseIfIdle(func() error { return nil }); e != nil {
		close(f.resume)
		<-done
		t.Fatal(e)
	}
	close(f.resume)
	if e := <-done; e == nil {
		t.Fatal("new remote effect dispatched after update admission closed")
	}
	f.mu.Lock()
	starts := f.starts
	f.mu.Unlock()
	if starts != 0 {
		t.Fatal("paused preparation reached native start", starts)
	}
	m.ResumeAfterUpdate()
	if _, e := m.StartTask(t.Context(), input("local-after-update")); e != nil {
		t.Fatal(e)
	}
}
