package caelis

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// workIntent extends the existing native command receipt with its authenticated
// host correlation. It contains no answers, file paths or independent run state.
type workIntent struct {
	TaskID      string `json:"taskID"`
	RequestID   string `json:"requestID"`
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint"`
}
type workIntentKey struct{}

func withWorkIntent(ctx context.Context, task, request, kind, fingerprint string) context.Context {
	return context.WithValue(ctx, workIntentKey{}, &workIntent{task, request, kind, fingerprint})
}
func workStopExecutionKey(state wire.SessionState) string {
	target := wire.TurnTarget{HandleId: value(state.Run.HandleId), RunId: value(state.Run.RunId), TurnId: value(state.Run.TurnId)}
	if target.HandleId == "" || target.RunId == "" || target.TurnId == "" {
		return ""
	}
	return api.WorkFingerprint(target)
}
func workSnapshotTail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[len(value)-limit:]
	for len(value) > 0 && !utf8.RuneStart(value[0]) {
		value = value[1:]
	}
	return value
}
func (s *Session) workCapabilitiesLocked(id string) (api.WorkCapabilities, error) {
	sid := s.state.Session.SessionId
	if id != "" {
		w, ok := s.state.Workers[id]
		if !ok {
			return api.WorkCapabilities{}, errors.New("task is not owned by this Worker service")
		}
		sid = w.Binding.SessionId
	}
	build := value(s.info.BuildId)
	if build == "" {
		build = value(s.info.DistributionVersion)
	}
	// These identify the native Session/store, not the App which happens to proxy it.
	owner := "caelis:" + api.WorkFingerprint([]string{s.state.StoreID, s.state.PrincipalID, sid})
	return api.WorkCapabilities{Contract: api.WorkerContract, Owner: owner, Build: build, Runtime: "caelis", History: true, Observe: true, Interactions: true, Files: true, Reconcile: true, URL: false, PendingAcrossRestart: false}, nil
}
func (s *Session) WorkCapabilities(_ context.Context, id string) (api.WorkCapabilities, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workCapabilitiesLocked(id)
}
func (s *Session) ObserveWork(ctx context.Context, id string) (api.WorkObservation, error) {
	if _, err := s.ReadWork(ctx, id); err != nil {
		return api.WorkObservation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.state.Workers[id]
	if !ok {
		return api.WorkObservation{}, errors.New("task is not owned")
	}
	caps, err := s.workCapabilitiesLocked(id)
	if err != nil {
		return api.WorkObservation{}, err
	}
	state := api.WorkState{Task: s.workerViewLocked(w), Runtime: "caelis", ExecutionKey: "", StopRequested: w.Stopped, Activity: w.Activity, StartFingerprint: w.Fingerprint}
	out := api.WorkObservation{State: state, Revision: strconv.FormatUint(s.revision, 10), Owner: caps, Items: []api.Item{}, Approvals: []api.Approval{}}
	if v := s.state.Views[w.Binding.SessionId]; v != nil {
		out.State.ExecutionKey = observedTurn(v)
		out.State.StopExecutionKey = workStopExecutionKey(v.State)
		out.State.TurnKey = observedTurn(v)
		// A bounded current view is enough for live output; explicit history uses the
		// native reader and is never confused with this window.
		projected := []api.Item{}
		dedicated := v.WorkerOutput != nil && v.WorkerOutput.Session == w.Binding.SessionId
		if dedicated {
			v.WorkerOutput.observeItems(v.Items)
			projected = v.WorkerOutput.current()
		}
		for _, item := range v.Items {
			// The resident projection groups text by Turn. Only the dedicated
			// Worker segmenter can prove a one-message native history identity.
			if dedicated && plainWorkerOutput(item) {
				continue
			}
			item.Text = workSnapshotTail(item.Text, 128<<10)
			item.Details = workSnapshotTail(item.Details, 64<<10)
			copy := clone(item)
			copy.AsyncCallID = item.AsyncCallID
			copy.AsyncQuestions = clone(item.AsyncQuestions)
			projected = append(projected, copy)
		}
		if dedicated {
			v.WorkerOutput.sortItems(projected)
		}
		used := 0
		for i := len(projected) - 1; i >= 0 && len(out.Items) < 200; i-- {
			item := projected[i]
			used += len(item.Text) + len(item.Details)
			if used > 512<<10 {
				break
			}
			out.Items = append(out.Items, item)
		}
		slices.Reverse(out.Items)
	}
	for _, a := range s.snapshotLocked().Approvals {
		if a.TaskID == id {
			out.Approvals = append(out.Approvals, a)
		}
	}
	return out, nil
}
func (s *Session) originalWorkOperationLocked(ctx context.Context, nativeID string, intent workIntent) (string, bool, error) {
	s.mu.Lock()
	journal, exists := s.state.Operations[nativeID]
	s.mu.Unlock()
	if !exists {
		return "", false, nil
	}
	if journal.Work == nil || *journal.Work != intent {
		return "", true, errors.New("original Worker operation identity or digest differs")
	}
	if journal.Outcome == "unknown" {
		_ = s.recoverOperation(ctx, nativeID)
		s.mu.Lock()
		journal = s.state.Operations[nativeID]
		s.mu.Unlock()
	}
	return productOutcome(wire.Outcome(journal.Outcome)), true, nil
}

// A native-target validation refusal proves no write occurred. Retain that
// fact in the same command receipts used for dispatched operations, so a lost
// host response cannot turn a rejected stale stop into permission to retry it.
func (s *Session) rejectWorkOperation(op string, intent workIntent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := intent
	s.state.Operations[op] = journal{Work: &copy, Digest: intent.Fingerprint, Outcome: "rejected"}
	return s.saveLocked()
}
func (s *Session) StopWorkRequest(ctx context.Context, in api.WorkStop) (api.Task, error) {
	if in.ID == "" || in.RequestID == "" || in.ExecutionKey == "" {
		return api.Task{}, errors.New("exact Worker stop target is required")
	}
	s.step.Lock()
	defer s.step.Unlock()
	intent := workIntent{in.ID, in.RequestID, "stop", api.WorkFingerprint(in)}
	op := "work-stop-" + digest([]byte(in.RequestID))
	outcome, exists, err := s.originalWorkOperationLocked(ctx, op, intent)
	s.mu.Lock()
	w, owned := s.state.Workers[in.ID]
	c := s.client
	connected := s.connected && !s.closed
	s.mu.Unlock()
	if !owned {
		return api.Task{}, errors.New("task is not owned")
	}
	if err != nil {
		return w.Task, err
	}
	if exists {
		s.mu.Lock()
		v := s.workerViewLocked(w)
		s.mu.Unlock()
		v.Outcome = outcome
		if outcome != "accepted" {
			return v, fmt.Errorf("original stop outcome is %s", outcome)
		}
		return v, nil
	}
	if !connected || c == nil || w.Binding.SessionId == "" {
		return w.Task, errors.New("original Worker owner is unavailable")
	}
	sid := w.Binding.SessionId
	var head wire.SessionState
	if err = c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/state", nil, &head, "", ""); err != nil {
		return w.Task, err
	}
	if head.SessionId != sid || workStopExecutionKey(head) != in.ExecutionKey || !value(head.Run.Active) {
		out := w.Task
		out.Outcome = "rejected"
		return out, errors.Join(errors.New("original Worker execution changed; stop was not dispatched"), s.rejectWorkOperation(op, intent))
	}
	target := wire.TurnTarget{HandleId: value(head.Run.HandleId), RunId: value(head.Run.RunId), TurnId: value(head.Run.TurnId)}
	if target.HandleId == "" || target.RunId == "" || target.TurnId == "" {
		return w.Task, errors.New("original native stop target is unconfirmed")
	}
	ctx = withWorkIntent(ctx, in.ID, in.RequestID, "stop", intent.Fingerprint)
	res, err := s.command(ctx, op, "/sessions/"+idPath(sid)+"/cancel", wire.CancelRequest{OperationId: &op, SessionId: &sid, Target: target})
	s.mu.Lock()
	defer s.mu.Unlock()
	if succeeded(res.Outcome) {
		w.Stopped = true
		s.state.Workers[in.ID] = w
		err = errors.Join(err, s.saveLocked())
		s.bumpLocked()
	}
	v := s.workerViewLocked(w)
	v.Outcome = productOutcome(res.Outcome)
	if !succeeded(res.Outcome) && err == nil {
		err = fmt.Errorf("original stop outcome is %s", v.Outcome)
	}
	return v, err
}
func (s *Session) DecideWork(ctx context.Context, in api.WorkDecision) error {
	if in.TaskID == "" || in.RequestID == "" || in.Decision.ID == "" {
		return &api.DecisionValidationError{Message: "exact Worker decision identity is required"}
	}
	s.step.Lock()
	defer s.step.Unlock()
	intent := workIntent{in.TaskID, in.RequestID, "decision", api.WorkFingerprint(in)}
	op := "work-decision-" + digest([]byte(in.RequestID))
	outcome, exists, err := s.originalWorkOperationLocked(ctx, op, intent)
	if err != nil {
		return err
	}
	if exists {
		if outcome == "accepted" {
			return nil
		}
		return fmt.Errorf("original decision outcome is %s", outcome)
	}
	return s.decideLocked(withWorkIntent(ctx, in.TaskID, in.RequestID, "decision", intent.Fingerprint), in.Decision, in.TaskID, op)
}
func (s *Session) WorkApprovalURL(_ context.Context, task, id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.state.Workers[task]
	ref, found := s.approvalLocked(id)
	if !ok || !found || ref.sid != w.Binding.SessionId {
		return "", errors.New("original Worker request is not pending")
	}
	return "", errors.New("this native Caelis owner does not support URL approval requests")
}
func (s *Session) ReadWorkOperation(ctx context.Context, q api.WorkOperationQuery) (api.WorkOperation, error) {
	out := api.WorkOperation{WorkOperationQuery: q, Outcome: "unknown"}
	if q.TaskID == "" || q.RequestID == "" || q.Fingerprint == "" || !slices.Contains([]string{"start", "send", "stop", "decision"}, q.Kind) {
		return out, errors.New("exact original operation and digest are required")
	}
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	w, ok := s.state.Workers[q.TaskID]
	s.mu.Unlock()
	if !ok {
		return out, errors.New("task is not owned")
	}
	intent := workIntent{q.TaskID, q.RequestID, q.Kind, q.Fingerprint}
	op := ""
	switch q.Kind {
	case "start":
		op = "work-prompt-" + digest([]byte(q.RequestID))
	case "send":
		op = "work-send-" + digest([]byte(q.RequestID))
	case "stop":
		op = "work-stop-" + digest([]byte(q.RequestID))
	case "decision":
		op = "work-decision-" + digest([]byte(q.RequestID))
	}
	outcome, exists, err := s.originalWorkOperationLocked(ctx, op, intent)
	if err != nil {
		return out, err
	}
	if exists {
		out.Outcome = outcome
		return out, nil
	}
	if q.Kind == "start" && w.HostStart != nil && *w.HostStart != intent {
		return out, errors.New("original Worker start digest differs")
	}
	// Absence is unknown, not proof that it is safe to dispatch again.
	return out, nil
}
func (s *Session) ReadWorkFile(ctx context.Context, task, id string) (api.WorkFileContent, error) {
	s.mu.Lock()
	w, owned := s.state.Workers[task]
	proven := false
	if v := s.state.Views[w.Binding.SessionId]; owned && v != nil {
		for _, item := range v.Items {
			for _, a := range item.Artifacts {
				if a.ID == id {
					proven = true
				}
			}
		}
	}
	s.mu.Unlock()
	if !owned {
		return api.WorkFileContent{}, errors.New("task is not owned")
	}
	if !proven {
		history, err := s.ReadWorkConversation(ctx, task)
		if err != nil {
			return api.WorkFileContent{}, err
		}
		for _, item := range history {
			for _, a := range item.Artifacts {
				if a.ID == id {
					proven = true
				}
			}
		}
	}
	parts := strings.Split(id, ":")
	if !proven || len(parts) != 3 || parts[0] != "resource" || parts[1] != w.Binding.SessionId {
		return api.WorkFileContent{}, errors.New("file does not belong to this native Worker")
	}
	data, resource, err := s.ReadResource(ctx, parts[1], parts[2])
	if err != nil {
		return api.WorkFileContent{}, err
	}
	return api.WorkFileContent{Name: resource.Name, MediaType: resource.MediaType, Data: data}, nil
}

var _ api.WorkCapabilityProvider = (*Session)(nil)
var _ api.WorkObserver = (*Session)(nil)
var _ api.WorkStopper = (*Session)(nil)
var _ api.WorkDecider = (*Session)(nil)
var _ api.WorkOperationReader = (*Session)(nil)
var _ api.WorkFileReader = (*Session)(nil)
