package codex

import (
	"context"
	"net/url"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerurl"
)

type workURLBinding struct {
	task    *taskRecord
	prompt  *prompt
	client  *Client
	page    *workerurl.Page
	loading chan struct{}
}

func (s *Session) workURLValidLocked(task string, b *workURLBinding) bool {
	if b == nil || s.closed || s.closing || s.binding.Tasks[task] != b.task || s.binding.LostOwnerTasks[task] || s.client != b.client || s.client == nil || s.state.Connection != "ready" || b.task.Owner != s.workOwnerLocked() {
		return false
	}
	p := b.prompt
	if p.sentChoice == "cancel" || p.sentChoice == "decline" {
		return false
	}
	if s.prompts[p.view.ID] == p && p.view.Status == "pending" {
		return true
	}
	// Native consent can resolve before the original web/tool challenge finishes.
	// It is not page completion or revocation. Terminal tool-turn facts do revoke
	// this accepted navigation channel, without expiring other pending requests.
	return p.nativeResolved && p.sentChoice == "accept" && !s.childTerminals[opaque(p.thread, p.turn)]
}
func (s *Session) workURLReceiptLocked(t *taskRecord, id, request string) api.WorkOperation {
	out := api.WorkOperation{WorkOperationQuery: api.WorkOperationQuery{TaskID: t.View.ID, RequestID: request, Kind: api.WorkURLSubmitKind}, Outcome: "unknown"}
	if r, ok := t.Requests[request]; ok && r.Kind == api.WorkURLSubmitKind && r.ControlTarget == id {
		out.Fingerprint = r.IntentFingerprint
		out.Outcome = r.Outcome
	}
	return out
}
func (s *Session) ReadWorkURLSubmission(ctx context.Context, task, id, request string) (api.WorkOperation, error) {
	if err := ctx.Err(); err != nil {
		return api.WorkOperation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return api.WorkOperation{}, workerurl.ErrUnavailable
	}
	t := s.binding.Tasks[task]
	if t == nil {
		return api.WorkOperation{}, workerurl.ErrUnavailable
	}
	// A missing/dead native request and an unknown original web effect can coexist.
	// Read the original receipt even after that request has gone; never resend it.
	return s.workURLReceiptLocked(t, id, request), nil
}
func (s *Session) ReadWorkURL(ctx context.Context, task, id string) (api.WorkURLPage, error) {
	if err := ctx.Err(); err != nil {
		return api.WorkURLPage{}, err
	}
	s.mu.Lock()
	t, p := s.binding.Tasks[task], s.prompts[id]
	key := opaque(task, id)
	if s.workURLPages == nil {
		s.workURLPages = map[string]*workURLBinding{}
	}
	b := s.workURLPages[key]
	if b == nil {
		if t == nil || p == nil || p.thread != t.Thread || p.view.TaskID != task || p.view.Status != "pending" || !safeWebURL(p.view.URL) {
			s.mu.Unlock()
			return api.WorkURLPage{}, workerurl.ErrUnavailable
		}
		// Reclaim only transient browser data, never receipts or native requests.
		for k, v := range s.workURLPages {
			if !s.workURLValidLocked(v.task.View.ID, v) {
				delete(s.workURLPages, k)
			}
		}
		if len(s.workURLPages) >= 32 {
			s.mu.Unlock()
			return api.WorkURLPage{}, workerurl.ErrUnavailable
		}
		b = &workURLBinding{task: t, prompt: p, client: s.client}
		s.workURLPages[key] = b
	}
	if !s.workURLValidLocked(task, b) {
		s.mu.Unlock()
		return api.WorkURLPage{}, workerurl.ErrUnavailable
	}
	raw := b.prompt.view.URL
	var prior *api.WorkURLReceipt
	for op, r := range t.Requests {
		if r.Kind == api.WorkURLSubmitKind && r.ControlTarget == id && r.Outcome != "rejected" {
			prior = &api.WorkURLReceipt{RequestID: op, Fingerprint: r.IntentFingerprint, Outcome: r.Outcome}
			break
		}
	}
	if prior != nil {
		v := api.WorkURLPage{Fields: []api.WorkURLField{}}
		if b.page != nil {
			v = b.page.View
		} else if u, err := url.Parse(raw); err == nil {
			v.Origin = u.Scheme + "://" + u.Host
		}
		v.Operation = prior
		s.mu.Unlock()
		return v, nil
	}
	if b.page != nil {
		v := b.page.View
		s.mu.Unlock()
		return v, nil
	}

	if b.loading != nil {
		done := b.loading
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return api.WorkURLPage{}, ctx.Err()
		case <-done:
			return s.ReadWorkURL(ctx, task, id)
		}
	}
	b.loading = make(chan struct{})
	s.mu.Unlock()
	page, err := workerurl.Fetch(ctx, raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	close(b.loading)
	b.loading = nil
	if err != nil {
		return api.WorkURLPage{}, err
	}
	if !s.workURLValidLocked(task, b) || s.workURLPages[key] != b {
		return api.WorkURLPage{}, workerurl.ErrUnavailable
	}
	if b.page == nil {
		b.page = page
	} // concurrent safe reads never change the offered revision
	return b.page.View, nil
}
func (s *Session) SubmitWorkURL(ctx context.Context, in api.WorkURLSubmission) (api.WorkOperation, error) {
	digest := api.WorkFingerprint(in)
	out := api.WorkOperation{WorkOperationQuery: api.WorkOperationQuery{TaskID: in.TaskID, RequestID: in.RequestID, Kind: api.WorkURLSubmitKind, Fingerprint: digest}, Outcome: "unknown"}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	s.mu.Lock()
	t := s.binding.Tasks[in.TaskID]
	if t == nil || !validWorkOperation(in.RequestID) {
		s.mu.Unlock()
		return out, workerurl.ErrInvalid
	}
	if r, ok := t.Requests[in.RequestID]; ok {
		s.mu.Unlock()
		if r.Kind != api.WorkURLSubmitKind || r.ControlTarget != in.ApprovalID || r.IntentFingerprint != digest {
			return out, workerurl.ErrInvalid
		}
		out.Outcome = r.Outcome
		return out, nil // accepted/unknown/rejected original receipt, no HTTP replay
	}
	reject := func() (api.WorkOperation, error) {
		if t.Requests == nil {
			t.Requests = map[string]taskReceipt{}
		}
		t.Requests[in.RequestID] = taskReceipt{Kind: api.WorkURLSubmitKind, ControlTarget: in.ApprovalID, IntentFingerprint: digest, Outcome: "rejected", Phase: "settled"}
		err := s.save()
		s.mu.Unlock()
		if err != nil {
			return out, err
		}
		out.Outcome = "rejected"
		return out, nil
	}
	b := s.workURLPages[opaque(in.TaskID, in.ApprovalID)]
	if !s.workURLValidLocked(in.TaskID, b) || b.page == nil || b.page.View.Revision != in.Revision {
		return reject()
	}
	for _, r := range t.Requests {
		if r.Kind == api.WorkURLSubmitKind && r.ControlTarget == in.ApprovalID && r.Outcome != "rejected" {
			return reject()
		}
	}
	body, err := b.page.Encode(in.Values)
	if err != nil {
		return reject()
	}
	if t.Requests == nil {
		t.Requests = map[string]taskReceipt{}
	}
	// Extend the original adapter receipt. No form values, nonce, cookies, HTML or
	// result body enter persistent state. Unknown is committed before HTTP dispatch.
	t.Requests[in.RequestID] = taskReceipt{Kind: api.WorkURLSubmitKind, ControlTarget: in.ApprovalID, IntentFingerprint: digest, Outcome: "unknown", Phase: "dispatching"}
	if err = s.save(); err != nil {
		s.mu.Unlock()
		return out, err
	} // a failed durable write cannot authorize dispatch
	s.mu.Unlock()
	err = b.page.Submit(ctx, body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding.Tasks[in.TaskID] != t {
		return out, nil
	} // archived/replaced projection cannot certify this receipt
	if err == nil {
		r := t.Requests[in.RequestID]
		r.Outcome = "accepted"
		r.Phase = "settled"
		t.Requests[in.RequestID] = r
		if err = s.save(); err != nil {
			r.Outcome = "unknown"
			r.Phase = "dispatching"
			t.Requests[in.RequestID] = r
			return out, err
		}
		out.Outcome = "accepted"
	}
	// A failed HTTP/timeout/read response can have an effect; query this exact
	// receipt, never a new submission. Native approval success is not inferred.
	return out, nil
}

var _ api.WorkURLChallenge = (*Session)(nil)
