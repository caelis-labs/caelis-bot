package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestReferenceObservationCancellationPreservesWriterAndRequestIdentity(t *testing.T) {
	a, b := net.Pipe()
	barrier := &referenceWriteBarrier{Conn: a, entered: make(chan struct{}), release: make(chan struct{})}
	rpc := newTransport(barrier, nil)
	t.Cleanup(func() { barrier.unblock(); b.Close(); rpc.close() })
	ctx, cancel := context.WithCancel(testContext(t))
	done := make(chan error, 1)
	go func() { _, err := rpc.observe(ctx, "skills/list", nil); done <- err }()
	<-barrier.entered
	cancel()
	barrier.unblock()
	d := json.NewDecoder(b)
	first := readWire(t, d)
	var request *RequestError
	if err := <-done; !errors.As(err, &request) || !request.OutcomeUnknown || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled observation lost its dispatched outcome", err)
	}
	if barrier.expired.Load() != 0 || rpc.cause() != nil {
		t.Fatal("observation cancellation destroyed the writer")
	}
	writeWire(t, b, wireMessage{ID: first.ID, Result: raw("late reference")})
	go func() { _, err := rpc.call(testContext(t), "cleanup", nil); done <- err }()
	next := readWire(t, d)
	if string(first.ID) == string(next.ID) || next.Method != "cleanup" {
		t.Fatal("late reference crossed request identity", next)
	}
	writeWire(t, b, wireMessage{ID: next.ID, Result: raw(map[string]any{})})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type admissionContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *admissionContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestQueuedReferenceCancellationDoesNotWrite(t *testing.T) {
	rpc, _ := pair(t)
	<-rpc.writeToken // Another frame owns the writer.
	defer func() { rpc.writeToken <- struct{}{} }()
	parent, cancel := context.WithCancel(testContext(t))
	ctx := &admissionContext{Context: parent, entered: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := rpc.observe(ctx, "skills/list", nil); done <- err }()
	<-ctx.entered
	cancel()
	select {
	case err := <-done:
		var request *RequestError
		if !errors.As(err, &request) || request.OutcomeUnknown || !errors.Is(err, context.Canceled) {
			t.Fatal("queued cancellation was dispatched", err)
		}
	case <-testContext(t).Done():
		t.Fatal("queued observation did not cancel")
	}
	if rpc.cause() != nil {
		t.Fatal("queued cancellation closed the transport")
	}
}

func TestStalledReferenceWriteKeepsOriginalDeadline(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	conn := &writeObserved{Conn: a, entered: make(chan struct{})}
	rpc := newTransport(conn, nil)
	defer rpc.close()
	ctx, cancel := context.WithTimeout(testContext(t), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := rpc.observe(ctx, "skills/list", nil); done <- err }()
	<-conn.entered
	select {
	case err := <-done:
		var request *RequestError
		if !errors.As(err, &request) || !request.OutcomeUnknown || err == nil || rpc.cause() == nil {
			t.Fatal("stalled frame did not fail under its deadline", err)
		}
	case <-testContext(t).Done():
		t.Fatal("reference write exceeded its deadline")
	}
}

type failingReferenceWrite struct {
	net.Conn
	written int
	err     error
	onWrite func()
}

func (c *failingReferenceWrite) Write([]byte) (int, error) {
	if c.onWrite != nil {
		c.onWrite()
	}
	return c.written, c.err
}

func TestFailedReferenceFramesStillCloseTransport(t *testing.T) {
	writeErr := errors.New("reference writer failed")
	for _, tc := range []struct {
		name    string
		written int
		err     error
		want    error
		cancel  bool
	}{
		{"zero bytes", 0, writeErr, writeErr, false},
		{"partial frame", 1, writeErr, writeErr, false},
		{"short write", 1, nil, io.ErrShortWrite, false},
		{"cancelled partial frame", 1, writeErr, writeErr, true},
		{"cancelled zero bytes", 0, writeErr, writeErr, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := net.Pipe()
			defer b.Close()
			ctx, cancel := context.WithCancel(testContext(t))
			defer cancel()
			conn := &failingReferenceWrite{Conn: a, written: tc.written, err: tc.err}
			if tc.cancel {
				conn.onWrite = cancel
			}
			rpc := newTransport(conn, nil)
			defer rpc.close()
			_, err := rpc.observe(ctx, "skills/list", nil)
			var request *RequestError
			if !errors.As(err, &request) || !request.OutcomeUnknown || !errors.Is(err, tc.want) || rpc.cause() == nil {
				t.Fatal("failed frame was hidden or transport reused", err)
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatal("write failure lost concurrent cancellation", err)
			}
		})
	}
}
