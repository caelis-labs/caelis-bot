package codex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
)

// These ports project the same native bindings used by local Workers. They do
// not import a resident chat or create a second execution journal.
func (s *Session) workOwnerLocked() string {
	if s.binding.OwnerEndpoint != "" {
		return "codex:" + opaque(s.binding.OwnerEndpoint)
	}
	if s.opts.Socket != "" {
		return "codex:" + opaque(s.opts.Socket)
	}
	return "codex:" + s.instance
}
func (s *Session) workBuildLocked() string {
	if s.client != nil {
		return s.client.build
	}
	return ""
}
func (s *Session) workExecutionKeyLocked(t *taskRecord) string {
	if t == nil {
		return ""
	}
	// A capacity retry has not acquired a Turn yet. Its original submission is
	// still a specific generation; a late stop cannot target the next one.
	run := t.Run
	if run == "" {
		run = "request:" + t.Pending
	}
	owner := t.Owner
	if owner == "" {
		owner = s.workOwnerLocked()
	}
	return opaque(owner, t.Thread, run)
}
func (s *Session) workCapabilitiesLocked(id string) (api.WorkCapabilities, error) {
	owner, build := s.workOwnerLocked(), s.workBuildLocked()
	if id != "" {
		t := s.binding.Tasks[id]
		if t == nil {
			return api.WorkCapabilities{}, errors.New("unknown Worker binding")
		}
		// Legacy bindings without this evidence cannot be upgraded by an App build.
		owner, build = t.Owner, t.Build
	}
	return api.WorkCapabilities{Contract: api.WorkerContract, Owner: owner, Build: build, Runtime: "codex", History: true, Observe: true, Interactions: true, Files: true, Reconcile: true, URL: true, PendingAcrossRestart: false}, nil
}
func (s *Session) WorkCapabilities(ctx context.Context, id string) (api.WorkCapabilities, error) {
	if err := ctx.Err(); err != nil {
		return api.WorkCapabilities{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workCapabilitiesLocked(id)
}
func (s *Session) ObserveWork(ctx context.Context, id string) (api.WorkObservation, error) {
	if err := ctx.Err(); err != nil {
		return api.WorkObservation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	states := s.workStatesLocked()
	t := s.binding.Tasks[id]
	if t == nil {
		return api.WorkObservation{}, errors.New("unknown Worker binding")
	}
	caps, err := s.workCapabilitiesLocked(id)
	if err != nil {
		return api.WorkObservation{}, err
	}
	out := api.WorkObservation{Owner: caps, Items: slices.Clone(s.workItems[id])}
	for _, state := range states {
		if state.Task.ID == id {
			out.State = state
			break
		}
	}
	live := map[string]bool{}
	for _, p := range s.state.Approvals {
		if p.TaskID == id {
			out.Approvals = append(out.Approvals, p)
			live[p.ID] = true
		}
	}
	for _, e := range t.ApprovalEvidence {
		if live[e.View.ID] || e.View.Status == "resolved" {
			continue
		}
		a := e.View
		a.TaskID = id
		a.NativeID = e.NativeID
		// Persisted evidence cannot recreate a pending native response channel.
		// Only a fresh request from the original live owner restores that authority.
		a.Status = "unknown"
		out.Approvals = append(out.Approvals, a)
	}

	type questionRevision struct {
		ID, CallID string
		Questions  []api.AsyncQuestion
	}
	questions := []questionRevision{}
	for _, item := range out.State.AsyncQuestions {
		questions = append(questions, questionRevision{item.ID, item.AsyncCallID, item.AsyncQuestions})
	}
	out.Revision = api.WorkFingerprint(struct {
		State     api.WorkState
		Approvals []api.Approval
		Items     []api.Item
		Questions []questionRevision
	}{out.State, out.Approvals, out.Items, questions})
	return out, nil
}

// A read returns only the original receipt and exact digest. It cannot replay.
func (s *Session) ReadWorkOperation(ctx context.Context, q api.WorkOperationQuery) (api.WorkOperation, error) {
	if err := ctx.Err(); err != nil {
		return api.WorkOperation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.binding.Tasks[q.TaskID]
	if t == nil {
		return api.WorkOperation{}, errors.New("unknown Worker binding")
	}
	r, ok := t.Requests[q.RequestID]
	if ok {
		r = s.reconcileControlReceiptLocked(t, q.RequestID)
	}
	if !ok {
		return api.WorkOperation{WorkOperationQuery: q, Outcome: "unknown"}, nil
	}
	if r.Kind != "" && r.Kind != q.Kind || r.IntentFingerprint != "" && r.IntentFingerprint != q.Fingerprint {
		return api.WorkOperation{}, errors.New("original operation intent does not match")
	}
	if r.IntentFingerprint == "" {
		return api.WorkOperation{WorkOperationQuery: q, Outcome: "unknown"}, nil
	}
	return api.WorkOperation{WorkOperationQuery: q, Outcome: r.Outcome}, nil
}

func workMessageFingerprint(in api.TaskMessage) string {
	if len(in.FileDigests) == 0 {
		return opaque(in.Prompt)
	}
	return opaque(in.Prompt, api.WorkFingerprint(in.FileDigests))
}

func validWorkOperation(id string) bool { return len(id) >= 8 && len(id) <= 128 }
func (s *Session) originalOperationLocked(t *taskRecord, id, kind, digest string) (bool, error) {
	if !validWorkOperation(id) {
		return false, errors.New("stable Worker operation ID required")
	}
	if t.Requests == nil {
		t.Requests = map[string]taskReceipt{}
	}
	if r, ok := t.Requests[id]; ok {
		if r.Kind != kind || r.IntentFingerprint != digest {
			return true, errors.New("original operation intent does not match")
		}
		if r.Outcome == "accepted" {
			return true, nil
		}
		if r.Outcome == "rejected" && kind == "decision" {
			return true, api.DecisionValidationError{Message: "原决定未发送，请核对该请求当前提供的选项"}
		}
		return true, fmt.Errorf("original operation outcome: %s; reconcile without replay", r.Outcome)
	}
	// Control traffic has no lifetime admission quota. Each entry is already a
	// minimal ID/intent digest/outcome, without answer text. Keep it so even old
	// or uncertain effects cannot be replayed, while history can never block stop.
	return false, nil
}

func (s *Session) DecideWork(ctx context.Context, in api.WorkDecision) error {
	ctx, cancel := s.operation(ctx, 15*time.Second)
	defer cancel()
	if err := lockwait.Lock(ctx, &s.op); err != nil {
		return err
	}
	defer s.op.Unlock()
	digest := api.WorkFingerprint(in)
	s.mu.Lock()
	t := s.binding.Tasks[in.TaskID]
	if t == nil || s.binding.LostOwnerTasks[in.TaskID] {
		s.mu.Unlock()
		return errors.New("original Worker owner unavailable")
	}
	if done, err := s.originalOperationLocked(t, in.RequestID, "decision", digest); done || err != nil {
		s.mu.Unlock()
		return err
	}
	p := s.prompts[in.Decision.ID]
	if p == nil || p.thread != t.Thread || p.view.TaskID != in.TaskID || p.view.Status != "pending" {
		t.Requests[in.RequestID] = taskReceipt{Kind: "decision", IntentFingerprint: digest, Outcome: "rejected", Phase: "settled"}
		if err := s.save(); err != nil {
			delete(t.Requests, in.RequestID)
			s.mu.Unlock()
			return err
		}
		s.mu.Unlock()
		return api.DecisionValidationError{Message: "original pending Worker request unavailable"}
	}
	t.Requests[in.RequestID] = taskReceipt{Kind: "decision", IntentFingerprint: digest, Outcome: "unknown", Phase: "dispatching"}
	if err := s.save(); err != nil {
		delete(t.Requests, in.RequestID)
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	err := s.decideLocked(ctx, in.Decision)
	s.mu.Lock()
	defer s.mu.Unlock()
	r := t.Requests[in.RequestID]
	if err == nil {
		r.Outcome = "accepted"
		r.Phase = "settled"
	} else {
		var invalid api.DecisionValidationError
		if errors.As(err, &invalid) {
			r.Outcome = "rejected"
			r.Phase = "settled"
		}
	}
	t.Requests[in.RequestID] = r
	if saveErr := s.save(); saveErr != nil {
		return saveErr
	}
	return err
}
func (s *Session) WorkApprovalURL(ctx context.Context, task, id string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.binding.Tasks[task]
	p := s.prompts[id]
	if t == nil || s.binding.LostOwnerTasks[task] || p == nil || p.thread != t.Thread || p.view.TaskID != task || p.view.Status != "pending" || !safeWebURL(p.view.URL) {
		return "", errors.New("original Worker authorization link unavailable")
	}
	return p.view.URL, nil
}

// workArtifacts resolves only citations in this exact native task workspace.
// The task's original transcript proves the reference; a global resident artifact
// map or a client-supplied path is never sufficient proof.
func workArtifacts(workspace, text string) ([]api.Artifact, map[string]string) {
	artifacts := []api.Artifact{}
	paths := map[string]string{}
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return artifacts, paths
	}
	for _, m := range artifactLink.FindAllStringSubmatch(text, -1) {
		path := strings.Trim(m[2], "<>")
		if u, e := url.Parse(path); !filepath.IsAbs(path) && e == nil && (u.Scheme != "" || strings.HasPrefix(path, "#")) {
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(workspace, path)
		}
		file, valid := existingArtifactFile(path)
		if !valid {
			continue
		}
		real, e := filepath.EvalSymlinks(file)
		if e != nil {
			continue
		}
		rel, e := filepath.Rel(root, real)
		if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.HasPrefix(rel, ".attachments"+string(filepath.Separator)) {
			continue
		}
		id := opaque("work-artifact", workspace, path)
		if _, exists := paths[id]; exists {
			continue
		}
		artifacts = append(artifacts, api.Artifact{ID: id, Name: filepath.Base(file)})
		paths[id] = real
	}
	return artifacts, paths
}
func (s *Session) ReadWorkFile(ctx context.Context, task, id string) (api.WorkFileContent, error) {
	if err := ctx.Err(); err != nil {
		return api.WorkFileContent{}, err
	}
	s.mu.Lock()
	t := s.binding.Tasks[task]
	workspace, path := "", ""
	if t != nil {
		workspace = t.View.Workspace
		path = t.Artifacts[id]
	}
	s.mu.Unlock()
	if t == nil {
		return api.WorkFileContent{}, errors.New("unknown Worker binding")
	}
	if path == "" {
		messages, err := s.ReadWorkConversation(ctx, task)
		if err != nil {
			return api.WorkFileContent{}, err
		}
		for _, m := range messages {
			_, paths := workArtifacts(workspace, m.Text)
			if p := paths[id]; p != "" {
				path = p
				break
			}
		}
	}
	if path == "" {
		return api.WorkFileContent{}, errors.New("file is not owned by this Worker")
	}
	// Revalidate the resolved native artifact inside this task's workspace before
	// opening bytes, including persisted proof after a host restart.
	root, e := filepath.EvalSymlinks(workspace)
	real, e2 := filepath.EvalSymlinks(path)
	rel, e3 := filepath.Rel(root, real)
	if e != nil || e2 != nil || e3 != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return api.WorkFileContent{}, errors.New("original Worker file unavailable")
	}
	path = real
	f, err := os.Open(path)
	if err != nil {
		return api.WorkFileContent{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > api.MaxWorkFileBytes {
		return api.WorkFileContent{}, errors.New("Worker file is not a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, api.MaxWorkFileBytes+1))
	if err != nil || len(data) > api.MaxWorkFileBytes {
		return api.WorkFileContent{}, errors.New("Worker file exceeds limit")
	}
	return api.WorkFileContent{Name: filepath.Base(path), MediaType: http.DetectContentType(data), Data: data}, nil
}

var _ api.WorkCapabilityProvider = (*Session)(nil)
var _ api.WorkObserver = (*Session)(nil)
var _ api.WorkDecider = (*Session)(nil)
var _ api.WorkOperationReader = (*Session)(nil)
var _ api.WorkFileReader = (*Session)(nil)

var _ api.WorkStopper = (*Session)(nil)

// Bounded observation keeps only this Worker's latest native items. It is rebuilt
// from native history after reconnect and never appears in the resident snapshot.
func (s *Session) observeWorkItemLocked(t *taskRecord, run string, item nativeItem, complete bool) {
	if t == nil || run == "" || item.ID == "" {
		return
	}
	if s.workItems == nil {
		s.workItems = map[string][]api.Item{}
	}
	view := api.Item{ID: opaque(t.Thread, run, item.ID), HistoryID: "msg-" + opaque(t.Thread, run, item.ID), TurnKey: opaque(t.Thread, run), Status: item.Status, Activity: itemActivity(item)}
	if complete {
		view.Status = "completed"
	} else if view.Status == "" {
		view.Status = "inProgress"
	}
	switch item.Type {
	case "agentMessage":
		view.Kind = "assistant"
		view.Text = boundedText(item.Text, 16000)
		if item.Delivery == "async" && len(item.Questions) > 0 {
			view.AsyncCallID = item.ID
			for _, q := range item.Questions {
				view.AsyncQuestions = append(view.AsyncQuestions, api.AsyncQuestion{Title: q.Title, Options: slices.Clone(q.Options)})
			}
		}
		if complete {
			view.Artifacts, _ = workArtifacts(t.View.Workspace, item.Text)
		}
	case "userMessage":
		view.Kind = "user"
		view.RequestID = item.ClientID
		parts := []string{}
		for _, p := range item.Content {
			if p.Type == "text" {
				parts = append(parts, p.Text)
			} else if p.Path != "" {
				parts = append(parts, "附件："+filepath.Base(p.Path))
			}
		}
		view.Text = boundedText(strings.Join(parts, "\n"), 16000)
	case "plan":
		view.Kind = "activity"
		view.Text = boundedText(item.Text, 16000)
	default:
		if view.Activity == nil {
			return
		}
		view.Kind = "activity"
		view.Text = boundedText(item.Output, 16000)
	}
	if complete {
		paths := map[string]string{}
		if item.Type == "agentMessage" {
			_, paths = workArtifacts(t.View.Workspace, item.Text)
		} else {
			candidates := []string{}
			if item.Type == "imageGeneration" || item.Type == "imageView" {
				candidates = append(candidates, item.Path)
			}
			if item.Type == "fileChange" {
				for _, change := range item.Changes {
					candidates = append(candidates, change.Path)
				}
			}
			for _, path := range candidates {
				as, ps := workArtifacts(t.View.Workspace, "[native output]("+path+")")
				view.Artifacts = append(view.Artifacts, as...)
				for id, p := range ps {
					paths[id] = p
				}
			}
		}
		if len(paths) > 0 {
			if t.Artifacts == nil {
				t.Artifacts = map[string]string{}
			}
			for id, path := range paths {
				t.Artifacts[id] = path
			}
			_ = s.save()
		}
	}
	items := s.workItems[t.View.ID]
	for i, old := range items {
		if old.ID == view.ID {
			if old.Status == "completed" && !complete {
				return
			}
			items[i] = view
			s.workItems[t.View.ID] = items
			return
		}
	}
	items = append(items, view)
	if len(items) > 64 {
		items = slices.Clone(items[len(items)-64:])
	}
	s.workItems[t.View.ID] = items
}
func (s *Session) observeWorkDeltaLocked(t *taskRecord, run, id, kind, delta string) {
	if t == nil || run == "" || id == "" || delta == "" {
		return
	}
	key := opaque(t.Thread, run, id)
	items := s.workItems[t.View.ID]
	for i, item := range items {
		if item.ID == key {
			if item.Status == "completed" {
				return
			}
			item.Text = boundedText(item.Text+delta, 16000)
			item.HistoryID = "msg-" + opaque(t.Thread, run, id)
			items[i] = item
			s.workItems[t.View.ID] = items
			return
		}
	}
	typ := "agentMessage"
	if kind == "item/plan/delta" {
		typ = "plan"
	}
	if kind == "item/commandExecution/outputDelta" {
		s.observeWorkItemLocked(t, run, nativeItem{ID: id, Type: "commandExecution", Output: delta, Status: "inProgress"}, false)
		return
	}
	s.observeWorkItemLocked(t, run, nativeItem{ID: id, Type: typ, Text: delta, Status: "inProgress"}, false)
}

type workerApprovalEvidence struct {
	View     api.Approval `json:"view"`
	NativeID string       `json:"nativeId"`
}

func (s *Session) retainWorkApprovalLocked(view api.Approval) {
	if view.TaskID == "" {
		return
	}
	t := s.binding.Tasks[view.TaskID]
	if t == nil {
		return
	}
	e := workerApprovalEvidence{View: view, NativeID: view.NativeID}
	found := false
	for i, old := range t.ApprovalEvidence {
		if old.View.ID == view.ID {
			t.ApprovalEvidence[i] = e
			found = true
			break
		}
	}
	if !found {
		t.ApprovalEvidence = append(t.ApprovalEvidence, e)
	}
	// Keep unresolved evidence even when the bounded resolved tail is evicted.
	if len(t.ApprovalEvidence) > 128 {
		for i, old := range t.ApprovalEvidence {
			if old.View.Status == "resolved" {
				t.ApprovalEvidence = slices.Delete(t.ApprovalEvidence, i, i+1)
				break
			}
		}
	}
	_ = s.save() // A failed audit write never grants response authority.
}

func (s *Session) reconcileControlReceiptLocked(t *taskRecord, id string) taskReceipt {
	r := t.Requests[id]
	if r.Kind == "stop" && r.Outcome == "unknown" && r.ControlTarget != "" {
		rejected := t.Requests[r.ControlTarget].Outcome == "rejected"
		acknowledged := t.StopPendingRequest == r.ControlTarget && t.StopPendingPhase == "acknowledged"
		if rejected || acknowledged {
			r.Outcome = "accepted"
			r.Phase = "settled"
			t.Requests[id] = r
			if err := s.save(); err != nil {
				r.Outcome = "unknown"
				r.Phase = "capacity"
				t.Requests[id] = r
			}
		}
	}
	return r
}
