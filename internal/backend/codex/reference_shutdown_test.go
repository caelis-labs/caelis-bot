package codex

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type referenceWriteBarrier struct {
	net.Conn
	entered, release chan struct{}
	releaseOnce      sync.Once
	expired          atomic.Int32
}

func (c *referenceWriteBarrier) Write(b []byte) (int, error) {
	if bytes.Contains(b, []byte(`"method":"skills/list"`)) {
		close(c.entered)
		<-c.release
	}
	return c.Conn.Write(b)
}

func (c *referenceWriteBarrier) SetWriteDeadline(d time.Time) error {
	if !d.IsZero() && !d.After(time.Now()) {
		c.expired.Add(1)
	}
	return c.Conn.SetWriteDeadline(d)
}

func (c *referenceWriteBarrier) unblock() { c.releaseOnce.Do(func() { close(c.release) }) }

// operation forwards lifecycle cancellation with AfterFunc. Observe completion
// of that forwarding callback, rather than merely the parent's Done channel.
type lifecycleCancelBarrier struct {
	context.Context
	cancelled chan struct{}
	once      sync.Once
}

// Route cancellation through AfterFunc instead of the parent's internal
// cancel-context shortcut. The lifecycle context carries no user values.
func (c *lifecycleCancelBarrier) Value(any) any { return nil }

func (c *lifecycleCancelBarrier) AfterFunc(f func()) func() bool {
	return context.AfterFunc(c.Context, func() {
		f()
		c.once.Do(func() { close(c.cancelled) })
	})
}

func TestCloseFinishesAdmittedReferenceFrameBeforeNativeCleanup(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared", true: "owned"}[owned], func(t *testing.T) {
			dir := t.TempDir()
			opts := SessionOptions{Directory: filepath.Join(dir, "work"), StateFile: filepath.Join(dir, "binding.json")}
			if err := os.WriteFile(opts.StateFile, raw(binding{Version: 1, ThreadID: "thread-native"}), 0600); err != nil {
				t.Fatal(err)
			}
			s := NewSession(opts)
			life := &lifecycleCancelBarrier{Context: s.life, cancelled: make(chan struct{})}
			s.life = life
			f := &sessionFixture{mode: "normal", started: make(chan struct{}, 8), answers: make(chan wireMessage, 8), loginReply: make(chan struct{}), history: []nativeTurn{{ID: "restored-turn", Status: "completed"}}}
			a, b := net.Pipe()
			barrier := &referenceWriteBarrier{Conn: a, entered: make(chan struct{}), release: make(chan struct{})}
			t.Cleanup(func() { barrier.unblock(); b.Close(); _ = s.Close(testContext(t)) })
			var stops atomic.Int32
			var stop func()
			if owned {
				stop = func() { stops.Add(1) }
			}
			s.start = func(context.Context, Options) (*Client, error) {
				f.peer = b
				go f.serve(b)
				return &Client{rpc: newTransportOptions(barrier, stop, true)}, nil
			}
			if err := s.Connect(testContext(t)); err != nil {
				t.Fatal(err)
			}
			if got := s.ConversationState(); !got.Observed || got.Turn != "restored-turn" {
				t.Fatal("native restoration not observed", got)
			}
			ctx := testContext(t)
			select {
			case <-barrier.entered:
			case <-ctx.Done():
				t.Fatal("reference frame was not admitted")
			}
			done := make(chan error, 1)
			go func() { done <- s.Close(ctx) }()
			select {
			case <-life.cancelled:
			case <-ctx.Done():
				t.Fatal("Close did not cancel session observations")
			}
			barrier.unblock()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("Close did not finish")
			}
			if barrier.expired.Load() != 0 {
				t.Fatal("Close interrupted the admitted reference frame")
			}
			f.mu.Lock()
			cleaned := f.cleaned
			f.mu.Unlock()
			if cleaned != 1 || stops.Load() != map[bool]int32{false: 0, true: 1}[owned] {
				t.Fatalf("cleanup=%d owned stops=%d", cleaned, stops.Load())
			}
		})
	}
}
