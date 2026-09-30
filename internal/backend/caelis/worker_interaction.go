package caelis

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (w *WorkerClient) WorkApprovals() []api.WorkApproval {
	s := w.engine
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.snapshotLocked()
	out := []api.WorkApproval{}
	for id, worker := range s.state.Workers {
		if worker.Task.Target == nil || *worker.Task.Target != s.workerTarget {
			continue
		}
		view := s.state.Views[worker.Binding.SessionId]
		if view == nil || view.State.Approval.Active == nil {
			continue
		}
		nativeID := approvalID(s.state.InstanceID, worker.Binding.SessionId, view.State.Approval.Active)
		for _, approval := range snapshot.Approvals {
			if approval.ID == nativeID {
				out = append(out, api.WorkApproval{TaskID: id, Target: s.workerTarget, Approval: approval})
			}
		}
	}
	slices.SortFunc(out, func(a, b api.WorkApproval) int {
		if n := strings.Compare(a.TaskID, b.TaskID); n != 0 {
			return n
		}
		return strings.Compare(a.Approval.ID, b.Approval.ID)
	})
	return out
}
func (w *WorkerClient) DecideWork(ctx context.Context, approval api.WorkApproval, decision api.Decision) error {
	if approval.Target != w.engine.workerTarget || decision.ID != approval.Approval.ID {
		return errors.New("Worker approval target mismatch")
	}
	found := false
	for _, current := range w.WorkApprovals() {
		if current.TaskID == approval.TaskID && current.Target == approval.Target && current.Approval.ID == decision.ID {
			found = true
			break
		}
	}
	if !found {
		return errors.New("Worker approval no longer belongs to this task")
	}
	// Decide reads the authoritative native head again and fences its exact turn,
	// request, permission payload and choice before issuing the original decision.
	return w.engine.Decide(ctx, decision)
}
func (w *WorkerClient) ReadWorkArtifact(ctx context.Context, taskID, artifactID string) (api.WorkArtifact, error) {
	var out api.WorkArtifact
	s := w.engine
	s.mu.Lock()
	worker, ok := s.state.Workers[taskID]
	owned := false
	if ok && worker.Task.Target != nil && *worker.Task.Target == s.workerTarget {
		if view := s.state.Views[worker.Binding.SessionId]; view != nil {
			for _, item := range view.Items {
				for _, artifact := range item.Artifacts {
					if artifact.ID == artifactID {
						owned = true
					}
				}
			}
		}
	}
	s.mu.Unlock()
	parts := strings.Split(artifactID, ":")
	if !owned || len(parts) != 3 || parts[0] != "resource" || parts[1] != worker.Binding.SessionId {
		return out, errors.New("artifact does not belong to this Worker task")
	}
	data, resource, err := s.ReadResource(ctx, parts[1], parts[2])
	if err != nil {
		return out, err
	}
	if len(data) > api.MaxWorkArtifactBytes {
		return out, errors.New("Worker artifact exceeds transfer limit")
	}
	return api.WorkArtifact{ID: artifactID, Name: resource.Name, MediaType: resource.MediaType, SHA256: resource.Sha256, Size: int64(resource.Size), Bytes: data}, nil
}

var _ api.WorkApprovalProvider = (*WorkerClient)(nil)
var _ api.WorkArtifactProvider = (*WorkerClient)(nil)

func (w *WorkerClient) WorkArtifacts() []api.WorkArtifactRef {
	s := w.engine
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []api.WorkArtifactRef{}
	seen := map[string]bool{}
	for taskID, worker := range s.state.Workers {
		if worker.Task.Target == nil || *worker.Task.Target != s.workerTarget {
			continue
		}
		if view := s.state.Views[worker.Binding.SessionId]; view != nil {
			for _, item := range view.Items {
				for _, artifact := range item.Artifacts {
					parts := strings.Split(artifact.ID, ":")
					if len(parts) != 3 || parts[0] != "resource" || parts[1] != worker.Binding.SessionId {
						continue
					}
					key := taskID + "\x00" + artifact.ID
					if seen[key] {
						continue
					}
					seen[key] = true
					out = append(out, api.WorkArtifactRef{TaskID: taskID, Target: s.workerTarget, Artifact: artifact})
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b api.WorkArtifactRef) int {
		if n := strings.Compare(a.TaskID, b.TaskID); n != 0 {
			return n
		}
		return strings.Compare(a.Artifact.ID, b.Artifact.ID)
	})
	return out
}

var _ api.WorkArtifactCatalog = (*WorkerClient)(nil)
