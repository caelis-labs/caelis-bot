package backend

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type recoveryEngineFixture struct {
	api.Engine
	mu           sync.Mutex
	state        api.RecoveryState
	calls        int
	observations int
	started      chan struct{}
	release      chan struct{}
	fail         error
}

func (e *recoveryEngineFixture) RecoveryState() api.RecoveryState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

func (e *recoveryEngineFixture) Connect(ctx context.Context) error {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	e.started <- struct{}{}
	select {
	case <-e.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fail == nil {
		e.state.Manual = false
	}
	return e.fail
}

func (e *recoveryEngineFixture) RetryObservations() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.observations++
}

func (*recoveryEngineFixture) Close(context.Context) error { return nil }

func TestRecoveryConnectSharesFlightAndRejectsStaleOwner(t *testing.T) {
	e := &recoveryEngineFixture{state: api.RecoveryState{Fence: "original-owner:7", Manual: true}, started: make(chan struct{}, 2), release: make(chan struct{})}
	s := NewService(e, nil, nil, nil, nil)
	initial := s.RecoveryState()
	if initial.Fence == "" || !initial.Manual {
		t.Fatal("manual recovery did not expose an opaque original-owner fence")
	}
	if err := s.RecoverIfCurrent(t.Context(), initial.Fence+"-stale"); err == nil {
		t.Fatal("stale owner reached native Connect")
	}
	first := make(chan error, 1)
	go func() { first <- s.RecoverIfCurrent(t.Context(), initial.Fence) }()
	select {
	case <-e.started:
	case <-time.After(time.Second):
		t.Fatal("manual recovery did not start")
	}
	if inFlight := s.RecoveryState(); !inFlight.InProgress || inFlight.Manual || inFlight.Fence == initial.Fence {
		t.Fatalf("in-flight recovery state = %+v", inFlight)
	}
	second := make(chan error, 1)
	go func() { second <- s.Connect(t.Context()) }() // Desktop uses the same admission.
	select {
	case <-e.started:
		t.Fatal("concurrent desktop action started a second native connection")
	case <-time.After(30 * time.Millisecond):
	}
	close(e.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverIfCurrent(t.Context(), initial.Fence); err == nil {
		t.Fatal("used recovery fence could be replayed")
	}
	e.mu.Lock()
	calls := e.calls
	e.mu.Unlock()
	if calls != 1 {
		t.Fatalf("native Connect calls = %d, want one", calls)
	}
}

func TestRecoveryAutomaticPriorityAndFailedAttemptGetsNewFence(t *testing.T) {
	e := &recoveryEngineFixture{state: api.RecoveryState{Fence: "owner:2", Automatic: true}, started: make(chan struct{}, 1), release: make(chan struct{}), fail: errors.New("offline")}
	s := NewService(e, nil, nil, nil, nil)
	if err := s.Connect(t.Context()); err == nil {
		t.Fatal("manual Connect preempted automatic recovery")
	}
	e.mu.Lock()
	e.state.Automatic, e.state.Manual = false, true
	e.mu.Unlock()
	old := s.RecoveryState().Fence
	close(e.release)
	if err := s.RecoverIfCurrent(t.Context(), old); err == nil {
		t.Fatal("fixture connection failure was hidden")
	}
	if next := s.RecoveryState(); !next.Manual || next.Fence == old {
		t.Fatalf("failed attempt did not rotate the manual fence: %+v", next)
	}
}

func TestRecoveryShutdownCancelsOriginalConnectionAttempt(t *testing.T) {
	e := &recoveryEngineFixture{state: api.RecoveryState{Fence: "owner:3", Manual: true}, started: make(chan struct{}, 1), release: make(chan struct{})}
	s := NewService(e, nil, nil, nil, nil)
	fence := s.RecoveryState().Fence
	done := make(chan error, 1)
	go func() { done <- s.RecoverIfCurrent(t.Context(), fence) }()
	select {
	case <-e.started:
	case <-time.After(time.Second):
		t.Fatal("manual recovery did not start")
	}
	if err := s.Shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("shutdown left recovery running: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel the original connection attempt")
	}
}

func TestOnlyExplicitSuccessfulRecoveryRenewsObservation(t *testing.T) {
	e := &recoveryEngineFixture{state: api.RecoveryState{Fence: "original-owner:1"}, started: make(chan struct{}, 3), release: make(chan struct{})}
	close(e.release)
	s := NewService(e, nil, nil, nil, nil)
	if err := s.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if e.observations != 0 {
		t.Fatal("ordinary Connect reset exhausted readers")
	}
	if err := s.Reconnect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if e.observations != 1 {
		t.Fatal("explicit recovery did not renew original readers")
	}
	e.fail = errors.New("offline")
	if err := s.Reconnect(t.Context()); err == nil {
		t.Fatal("failed recovery reported success")
	}
	if e.observations != 1 {
		t.Fatal("failed connection renewed observation")
	}
}

func TestExplicitObservationRecoveryJoinsExistingConnectionFlight(t *testing.T) {
	e := &recoveryEngineFixture{state: api.RecoveryState{Fence: "original-owner:1"}, started: make(chan struct{}, 2), release: make(chan struct{})}
	s := NewService(e, nil, nil, nil, nil)
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- s.Connect(t.Context()) }()
	select {
	case <-e.started:
	case <-time.After(time.Second):
		t.Fatal("Connect did not start")
	}
	go func() { second <- s.Reconnect(t.Context()) }()
	joined := false
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.recoveryMu.Lock()
		joined = s.recoveryFlight != nil && s.recoveryFlight.retryObservations
		s.recoveryMu.Unlock()
		if joined {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(e.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if !joined || e.calls != 1 || e.observations != 1 {
		t.Fatal("joined explicit recovery lost or repeated", joined, e.calls, e.observations)
	}
}
