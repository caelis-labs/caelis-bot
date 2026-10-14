package codex

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
)

func preparePrivateCloseFixture(t *testing.T, s *Session, endpoint string, id ownerProcessIdentity) {
	t.Helper()
	s.mu.Lock()
	s.client.owner = &fixtureRetainedOwner{endpoint: endpoint, identity: id}
	s.binding.OwnerEndpoint, s.binding.OwnerProcess = endpoint, &id
	s.binding.CapacityStopRequest, s.binding.CapacityStopThread = "original-close-request", "thread-native"
	s.binding.CapacityStopPhase = "requested"
	s.state.Connection = "offline"
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
}

func TestPrivateCloseIntentCrashKeepsLivingOwnerAndUnknownLifecycleIsolated(t *testing.T) {
	s, _ := sessionPair(t, "hold")
	endpoint := "unix://" + filepath.Join(t.TempDir(), "still-live.sock")
	id := ownerProcessIdentity{PID: 1401, Birth: 401}
	preparePrivateCloseFixture(t, s, endpoint, id)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	s.mu.Lock()
	s.client.rpc.stop = func() { close(entered); <-release }
	s.ownerLifecycleProbe = func(ownerProcessIdentity) ownerLifecycle { return ownerAlive }
	s.mu.Unlock()
	closeCtx := testContext(t)
	go func() { done <- s.Close(closeCtx) }()
	select {
	case <-entered: // Close wrote intent and reached the process-stop boundary.
	case <-time.After(2 * time.Second):
		t.Fatal("private Close did not reach the stop boundary")
	}
	unknown := NewSession(s.opts)
	defer unknown.cancelLife()
	if !unknown.binding.OwnerClosing || unknown.binding.OwnerUnavailable || unknown.binding.OwnerProcess == nil || *unknown.binding.OwnerProcess != id {
		t.Fatal("pre-stop crash window persisted a false exited fact", unknown.binding)
	}
	unknown.ownerLifecycleProbe = func(ownerProcessIdentity) ownerLifecycle { return ownerUnknown }
	var unknownStarts atomic.Int32
	unknown.start = func(context.Context, Options) (*Client, error) { unknownStarts.Add(1); return nil, context.Canceled }
	if err := unknown.Connect(testContext(t)); err == nil || unknown.Snapshot().ConnectionIssue != "owner_lifecycle_unknown" {
		t.Fatal("unverifiable owner escaped quarantine", err, unknown.Snapshot())
	}
	service := backend.NewService(unknown, nil, nil, nil, nil)
	if err := service.RecoverIfCurrent(testContext(t), service.RecoveryState().Fence); err == nil || unknownStarts.Load() != 0 || len(unknown.binding.LostOwners) != 0 {
		t.Fatal("manual recovery inferred death from closing intent", err, unknownStarts.Load())
	}
	live := NewSession(s.opts)
	defer func() { _ = live.Close(testContext(t)) }()
	live.ownerLifecycleProbe = func(ownerProcessIdentity) ownerLifecycle { return ownerAlive }
	start := s.start
	var originalSocket atomic.Bool
	live.start = func(ctx context.Context, opts Options) (*Client, error) {
		if opts.RequiredSocket && opts.Socket == strings.TrimPrefix(endpoint, "unix://") {
			originalSocket.Store(true)
		}
		return start(ctx, opts)
	}
	if err := live.Connect(testContext(t)); err != nil || !originalSocket.Load() || live.binding.OwnerUnavailable || live.binding.OwnerClosing || len(live.binding.LostOwners) != 0 {
		t.Fatal("surviving owner was archived or replaced instead of reattached", err, originalSocket.Load(), live.binding)
	}
	close(release)
	select {
	case err := <-done:
		if err == nil { // The fixture deliberately leaves its original process alive.
			t.Fatal("Close claimed a private owner had exited without lifecycle evidence")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Close did not finish")
	}
}

func TestPersistedUnavailableMarkerCannotArchiveLivingOriginalOwner(t *testing.T) {
	s, _ := sessionPair(t, "hold")
	endpoint := "unix://" + filepath.Join(t.TempDir(), "old-marker-live.sock")
	id := ownerProcessIdentity{PID: 1402, Birth: 402}
	preparePrivateCloseFixture(t, s, endpoint, id)
	s.mu.Lock()
	s.binding.OwnerUnavailable = true // Earlier code saved this before stopping the process.
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	loaded := NewSession(s.opts)
	defer func() { _ = loaded.Close(testContext(t)) }()
	loaded.ownerLifecycleProbe = func(ownerProcessIdentity) ownerLifecycle { return ownerAlive }
	start := s.start
	var originalSocket atomic.Bool
	loaded.start = func(ctx context.Context, opts Options) (*Client, error) {
		originalSocket.Store(opts.RequiredSocket && opts.Socket == strings.TrimPrefix(endpoint, "unix://"))
		return start(ctx, opts)
	}
	service := backend.NewService(loaded, nil, nil, nil, nil)
	if err := service.RecoverIfCurrent(testContext(t), service.RecoveryState().Fence); err != nil || !originalSocket.Load() || len(loaded.binding.LostOwners) != 0 {
		t.Fatal("persisted exit marker overrode a live original process", err, originalSocket.Load(), loaded.binding.LostOwners)
	}
}

func TestPrivateClosePostStopWriteFailureRechecksGoneOwnerBeforeRecovery(t *testing.T) {
	s, _ := sessionPair(t, "hold")
	endpoint := "unix://" + filepath.Join(t.TempDir(), "exited-owner.sock")
	id := ownerProcessIdentity{PID: 1403, Birth: 403}
	preparePrivateCloseFixture(t, s, endpoint, id)
	stateFile := s.opts.StateFile
	var stopped atomic.Bool
	s.mu.Lock()
	s.ownerLifecycleProbe = func(ownerProcessIdentity) ownerLifecycle {
		if stopped.Load() {
			return ownerGone
		}
		return ownerAlive
	}
	s.client.rpc.stop = func() {
		stopped.Store(true)
		s.mu.Lock()
		s.opts.StateFile = filepath.Dir(stateFile) // The second atomic write must fail.
		s.mu.Unlock()
	}
	s.mu.Unlock()
	if err := s.Close(testContext(t)); err == nil || !strings.Contains(err.Error(), "保存") {
		t.Fatal("post-stop write failure was hidden", err)
	}
	loaded := NewSession(SessionOptions{Directory: s.opts.Directory, StateFile: stateFile})
	defer func() { _ = loaded.Close(testContext(t)) }()
	if !loaded.binding.OwnerClosing || loaded.binding.OwnerUnavailable || loaded.binding.OwnerProcess == nil || *loaded.binding.OwnerProcess != id {
		t.Fatal("post-stop failed write lost the durable close intent", loaded.binding)
	}
	loaded.ownerLifecycleProbe = func(ownerProcessIdentity) ownerLifecycle { return ownerGone }
	start := s.start
	var starts atomic.Int32
	loaded.start = func(ctx context.Context, opts Options) (*Client, error) { starts.Add(1); return start(ctx, opts) }
	if err := loaded.Connect(testContext(t)); err == nil || starts.Load() != 0 || loaded.Snapshot().ConnectionIssue != "owner_unavailable" {
		t.Fatal("owner death was not durably verified before new connection", err, starts.Load(), loaded.Snapshot())
	}
	reloaded := NewSession(loaded.opts)
	defer reloaded.cancelLife()
	if !reloaded.binding.OwnerUnavailable || reloaded.binding.OwnerClosing || reloaded.binding.CapacityStopRequest != "original-close-request" {
		t.Fatal("verified death or original request did not survive reload", reloaded.binding)
	}
	service := backend.NewService(loaded, nil, nil, nil, nil)
	if err := service.RecoverIfCurrent(testContext(t), service.RecoveryState().Fence); err != nil || starts.Load() != 1 || len(loaded.binding.LostOwners) != 1 {
		t.Fatal("fenced recovery did not establish a separate usable owner", err, starts.Load(), loaded.binding.LostOwners)
	}
}
