package machines

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

type controlDeadlineReviewTransport struct {
	t       *testing.T
	maximum time.Duration
	calls   int
}

func (rt *controlDeadlineReviewTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.calls++
	d, ok := r.Context().Deadline()
	remaining := time.Until(d)
	if !ok || remaining <= 0 || remaining > rt.maximum {
		rt.t.Errorf("peer decision inherited unbounded/general Web lifetime: deadline=%v remaining=%v max=%v", ok, remaining, rt.maximum)
	}
	var in workerpeer.Request
	if e := json.NewDecoder(r.Body).Decode(&in); e != nil {
		rt.t.Fatal(e)
	}
	if in.Action != "decision" || in.Decision.TaskID != "original-task" || in.Decision.RequestID != "original-operation" || in.Decision.Decision.ID != "original-native-request" {
		rt.t.Fatal("deadline changed original identity", in.Action, in.Decision)
	}
	// Controlled transport uncertainty must not become a certified rejection or
	// a second request. No socket, timer wait, or native fixture process needed.
	return nil, context.DeadlineExceeded
}
func TestReviewPeerDecisionUsesBoundedOriginalContextAndKeepsUnknown(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(map[bool]string{false: "Web-lifetime", true: "earlier-parent"}[parent], func(t *testing.T) {
			s, e := Open(t.TempDir(), &dualPeerLocal{})
			if e != nil {
				t.Fatal(e)
			}
			s.state.Profiles["B"] = profile{View: api.Machine{ID: "B", ConnectionType: "bot"}, Peer: workerpeer.Connection{Address: "http://127.0.0.1:1", Peer: "original-peer", Token: "controlled-proof"}}
			s.state.Routes["original-task"] = "B"
			s.state.Runtimes["original-task"] = "codex"
			ctx := t.Context()
			maximum := 8 * time.Second
			if parent {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Second)
				defer cancel()
				maximum = time.Second
			}
			rt := &controlDeadlineReviewTransport{t: t, maximum: maximum}
			previous := http.DefaultTransport
			http.DefaultTransport = rt
			defer func() { http.DefaultTransport = previous }()
			e = s.DecideWork(ctx, api.WorkDecision{TaskID: "original-task", RequestID: "original-operation", Decision: api.Decision{ID: "original-native-request", Choice: "approve"}})
			var rejected *api.ActionRejectedError
			var invalid api.DecisionValidationError
			if e == nil || errors.As(e, &rejected) || errors.As(e, &invalid) || rt.calls != 1 {
				t.Fatal("uncertain timeout was rejected/replayed/accepted", e, rt.calls)
			}
		})
	}
}
