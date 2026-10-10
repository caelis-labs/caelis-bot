package sharedruntime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentRecoveryMergesAndBacksOff(t *testing.T) {
	for _, outcome := range []error{nil, errors.New("start failed")} {
		c := &Coordinator{}
		var starts atomic.Int32
		entered, release := make(chan struct{}), make(chan struct{})
		start := func(context.Context) error {
			if starts.Add(1) == 1 {
				close(entered)
			}
			<-release
			return outcome
		}
		results := make(chan error, 32)
		for range 32 {
			go func() { results <- c.Recover(t.Context(), "same-owner", start) }()
		}
		<-entered
		close(release)
		for range 32 {
			if err := <-results; !errors.Is(err, outcome) {
				t.Fatal(err)
			}
		}
		if starts.Load() != 1 {
			t.Fatal("concurrent recovery launched duplicate services", starts.Load())
		}
		if err := c.Recover(t.Context(), "same-owner", start); !errors.Is(err, outcome) || starts.Load() != 1 {
			t.Fatal("quiet/fast retry bypassed cooldown", err, starts.Load())
		}
		// Advance the completed attempt, not wall-clock time, to exercise retry.
		c.mu.Lock()
		c.attempts["same-owner"].next = time.Now().Add(-time.Second)
		c.mu.Unlock()
		_ = c.Recover(t.Context(), "same-owner", start)
		if starts.Load() != 2 {
			t.Fatal("expired cooldown never retried")
		}
		_ = c.Recover(t.Context(), "different-owner", start)
		if starts.Load() != 3 {
			t.Fatal("one failed service blocked an independent owner")
		}
	}
}

func TestRecoveryCancellationDoesNotLaunchOrCancelAnotherObserver(t *testing.T) {
	c := &Coordinator{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.Recover(ctx, "original", func(context.Context) error { t.Fatal("launched after stop"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- c.Recover(t.Context(), "original", func(context.Context) error { close(entered); <-release; return nil })
	}()
	<-entered
	join, stop := context.WithCancel(t.Context())
	joined := make(chan error, 1)
	go func() {
		joined <- c.Recover(join, "original", func(context.Context) error { t.Error("duplicate start"); return nil })
	}()
	stop()
	if err := <-joined; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal("detached observer cancelled another owner", err)
	}
}

func TestFirstObserverCancellationKeepsSharedStartup(t *testing.T) {
	c := &Coordinator{}
	first, cancel := context.WithCancel(t.Context())
	started, release := make(chan context.Context, 1), make(chan struct{})
	var starts atomic.Int32
	start := func(ctx context.Context) error {
		starts.Add(1)
		started <- ctx
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- c.Recover(first, "original", start) }()
	sharedCtx := <-started
	if _, bounded := sharedCtx.Deadline(); !bounded {
		t.Fatal("shared startup has no deadline")
	}
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatal("first observer did not stop waiting", err)
	}
	if err := sharedCtx.Err(); err != nil {
		t.Fatal("first observer canceled the shared startup", err)
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- c.Recover(t.Context(), "original", start) }()
	close(release)
	if err := <-secondDone; err != nil {
		t.Fatal("active observer lost the shared startup", err)
	}
	if err := c.Recover(t.Context(), "original", start); err != nil || starts.Load() != 1 {
		t.Fatal("observer cancellation was cached or launched another startup", err, starts.Load())
	}
}
