package machines

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

// Only native capacity waiting is controlled. Manager, routing, authenticated
// HTTP, generation selection and the guarded stop path are the real product.
type inflightStopNative struct {
	*dualPeerNative
	waiting  atomic.Bool
	entered  chan struct{}
	released chan struct{}
	once     sync.Once
}

func (o *inflightStopNative) Connect(ctx context.Context) error {
	if o.waiting.Load() {
		<-ctx.Done()
		return ctx.Err()
	}
	return o.dualPeerNative.Connect(ctx)
}
func (o *inflightStopNative) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	o.waiting.Store(true)
	close(o.entered)
	select {
	case <-o.released:
	case <-ctx.Done():
	}
	o.waiting.Store(false)
	v, _ := o.dualPeerNative.ReadWork(ctx, in.ID)
	return v, &api.WorkMessageRejection{Cause: errors.New("capacity wait cancelled before dispatch")}
}
func (o *inflightStopNative) StopWorkRequest(ctx context.Context, in api.WorkStop) (api.Task, error) {
	v, err := o.dualPeerNative.StopWorkRequest(ctx, in)
	if err == nil {
		o.once.Do(func() { close(o.released) })
	}
	return v, err
}
func TestRemoteInflightStopReachesOriginalNativeWithoutWriteLocks(t *testing.T) {
	o := &inflightStopNative{dualPeerNative: newDualNative("source", "codex"), entered: make(chan struct{}), released: make(chan struct{})}
	t.Cleanup(func() { o.once.Do(func() { close(o.released) }) })
	peer, err := workerpeer.Open(t.TempDir(), func(context.Context, string, string, string) (workerpeer.Owner, string, *api.WorkExecutionSettings, error) {
		return o, "codex", &api.WorkExecutionSettings{Model: "B-codex"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	target := &dualPeerTarget{peer: peer, http: httptest.NewServer(peer)}
	defer target.http.Close()
	service, manager, _, machine, _ := dualPeerConnect(t, target)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	first, err := manager.StartTask(ctx, api.TaskStart{RequestID: "original-start", Title: "Stop capacity", Prompt: "one", Machine: machine})
	if err != nil {
		t.Fatal(err)
	}
	o.mu.Lock()
	v := o.states[first.ID]
	v.Task.Status = "completed"
	v.Activity = "idle"
	o.states[first.ID] = v
	o.mu.Unlock()
	if _, err = manager.ReadTask(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	sent := make(chan error, 1)
	go func() {
		_, e := manager.SendTask(ctx, api.TaskMessage{ID: first.ID, RequestID: "original-continue", Prompt: "continue"})
		sent <- e
	}()
	select {
	case <-o.entered:
	case <-ctx.Done():
		t.Fatal("send did not reach original native", ctx.Err())
	}
	stopped := make(chan error, 1)
	go func() { _, e := manager.StopTask(ctx, first.ID); stopped <- e }()
	select {
	case err = <-stopped:
		if err != nil {
			t.Error(err)
		}
	case <-ctx.Done():
		t.Error("guarded stop waited behind original pending write")
	}
	o.once.Do(func() { close(o.released) })
	<-sent
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stops != 1 || o.sends != 0 {
		t.Errorf("native stop=%d new input effects=%d", o.stops, o.sends)
	}
	_ = service
}
