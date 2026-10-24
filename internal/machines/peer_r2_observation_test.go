package machines

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

// Only the native boundary is controlled. Product routing, receipts, state
// polling, the subscription budget and retained UI projection are production.
type r2ObservedNative struct{ *dualPeerNative }

func (n *r2ObservedNative) ObserveWork(ctx context.Context, id string) (api.WorkObservation, error) {
	v, err := n.dualPeerNative.ObserveWork(ctx, id)
	if v.State.Task.Status == "completed" {
		v.Approvals = nil
	}
	return v, err
}

type r2StreamCounters struct {
	active, maximum, attempts   atomic.Int32
	clientActive, clientMaximum atomic.Int32
}

func r2ObservationTarget(t *testing.T, failEvents bool) (*dualPeerTarget, *r2StreamCounters) {
	t.Helper()
	f := &dualPeerTarget{defaultRuntime: "codex", owners: map[string]*dualPeerNative{}}
	s, err := workerpeer.Open(t.TempDir(), func(_ context.Context, source, runtime, _ string) (workerpeer.Owner, string, *api.WorkExecutionSettings, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if runtime == "" {
			runtime = f.defaultRuntime
		}
		key := source + "\x00" + runtime
		if f.owners[key] == nil {
			f.owners[key] = newDualNative(source, runtime)
		}
		return &r2ObservedNative{f.owners[key]}, runtime, &api.WorkExecutionSettings{Model: "B-" + runtime}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.peer = s
	if err = s.Enable(true); err != nil {
		t.Fatal(err)
	}
	streams := new(r2StreamCounters)
	f.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == workerpeer.Prefix+"events" {
			streams.attempts.Add(1)
			n := streams.active.Add(1)
			defer streams.active.Add(-1)
			for old := streams.maximum.Load(); n > old && !streams.maximum.CompareAndSwap(old, n); old = streams.maximum.Load() {
			}
			if failEvents {
				http.Error(w, "fixture stream unavailable", 503)
				return
			}
		}
		s.ServeHTTP(w, r)
	}))
	// Count the actual A-side HTTP lifetime, including connection and Body.Close.
	// A canceled B handler can still be queued for teardown after A has closed its
	// socket; that remote scheduling delay is not a live client subscription.
	previous := http.DefaultTransport
	http.DefaultTransport = &r2ClientTransport{upstream: previous, peer: f.http.URL, counters: streams}
	t.Cleanup(func() { http.DefaultTransport = previous })
	t.Cleanup(f.http.Close)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return f, streams
}
func r2ApprovalCount(s *Service) int {
	approvals, _ := s.RetainedApprovalState()
	return len(approvals)
}
func r2WaitApprovals(t *testing.T, s *Service, want int) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if r2ApprovalCount(s) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pending approvals depend on SSE slots: got %d want %d", r2ApprovalCount(s), want)
}
func TestPeerAllApprovalsArriveWithoutDetailBeyondStreamBudget(t *testing.T) {
	for _, failEvents := range []bool{false, true} {
		t.Run(fmt.Sprint("events-failed=", failEvents), func(t *testing.T) {
			target, streams := r2ObservationTarget(t, failEvents)
			s, m, _, machine, _ := dualPeerConnect(t, target)
			m.ConfigureLimit(func() int { return 20 })
			for i := range 17 {
				if _, err := m.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: fmt.Sprintf("active-%d", i), Prompt: "native approval"}); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			go func() { defer close(done); s.Observe(ctx) }()
			defer func() { cancel(); <-done }()
			r2WaitApprovals(t, s, 17)
			if streams.attempts.Load() == 0 {
				t.Fatal("fixture never exercised the SSE transport")
			}
			if streams.clientMaximum.Load() > 16 {
				t.Fatal("subscription budget increased", streams.clientMaximum.Load())
			}
		})
	}
}
func TestPeerHistoricalQuestionsDoNotStarveOnlyActiveApproval(t *testing.T) {
	target, streams := r2ObservationTarget(t, false)
	s, m, _, machine, _ := dualPeerConnect(t, target)
	owner := target.owner(s.state.ProfileID, "codex")
	for i := range 16 {
		v, err := m.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: fmt.Sprintf("historical-%d", i), Prompt: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		owner.mu.Lock()
		state := owner.states[v.ID]
		state.Task.Status = "completed"
		state.Activity = "idle"
		state.AsyncQuestions = []api.Item{{ID: "historical-question", Status: "completed", AsyncCallID: "closed-native-call", AsyncQuestions: []api.AsyncQuestion{{Title: "already answered"}}}}
		owner.states[v.ID] = state
		owner.mu.Unlock()
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); s.Observe(ctx) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		s.cacheMu.RLock()
		n := len(s.observations)
		s.cacheMu.RUnlock()
		if n == 16 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := m.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "only-active", Prompt: "approval"}); err != nil {
		t.Fatal(err)
	}
	r2WaitApprovals(t, s, 1)
	// Historical question records no longer reserve native event subscriptions.
	waitPeerTest(t, func() bool { return streams.active.Load() == 1 })
	var completed []string
	s.cacheMu.RLock()
	for id, observation := range s.observations {
		if observation.State.Task.Status == "completed" {
			completed = append(completed, id)
		}
	}
	s.cacheMu.RUnlock()
	if len(completed) != 16 {
		t.Fatal("historical observations missing", len(completed))
	}
	for _, id := range completed {
		if s.needsPeerStream(id) {
			t.Fatal("completed historical question retained a subscription", id)
		}
	}
	if streams.clientMaximum.Load() > 16 {
		t.Fatal("subscription budget increased", streams.clientMaximum.Load())
	}
}

type r2ClientTransport struct {
	upstream http.RoundTripper
	peer     string
	counters *r2StreamCounters
}

func (rt *r2ClientTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme+"://"+r.URL.Host != rt.peer || r.URL.Path != workerpeer.Prefix+"events" {
		return rt.upstream.RoundTrip(r)
	}
	n := rt.counters.clientActive.Add(1)
	for old := rt.counters.clientMaximum.Load(); n > old && !rt.counters.clientMaximum.CompareAndSwap(old, n); old = rt.counters.clientMaximum.Load() {
	}
	var once sync.Once
	release := func() { once.Do(func() { rt.counters.clientActive.Add(-1) }) }
	response, err := rt.upstream.RoundTrip(r)
	if err != nil {
		release()
		return nil, err
	}
	response.Body = &r2ClientBody{ReadCloser: response.Body, release: release}
	return response, nil
}

type r2ClientBody struct {
	io.ReadCloser
	release func()
}

func (b *r2ClientBody) Close() error { defer b.release(); return b.ReadCloser.Close() }
