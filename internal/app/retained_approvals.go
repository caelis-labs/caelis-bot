package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"slices"
)

type approvalObserver interface {
	Snapshot() api.Snapshot
	Decide(context.Context, api.Decision) error
}

// Only independently owned Worker approvals cross the resident boundary.
// Native IDs and choices are passed unchanged to the exact original adapter.
func (w *localWorkers) RetainedApprovals() []api.Approval {
	v, _ := w.RetainedApprovalState()
	return v
}

func (w *localWorkers) RetainedApprovalState() ([]api.Approval, uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := []api.Approval{}
	for id, owner := range w.owners {
		if id == w.active {
			continue
		}
		if source, ok := owner.(approvalObserver); ok {
			for _, v := range source.Snapshot().Approvals {
				if v.Owner == "task" {
					out = append(out, v)
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b api.Approval) int { return cmp.Compare(a.ID, b.ID) })
	encoded, _ := json.Marshal(out)
	if string(encoded) != w.approvalFingerprint {
		w.approvalFingerprint = string(encoded)
		w.approvalRevision++
	}
	return out, w.approvalRevision
}

func (w *localWorkers) DecideRetainedApproval(ctx context.Context, d api.Decision) (bool, error) {
	w.mu.Lock()
	var target approvalObserver
	for id, owner := range w.owners {
		if id == w.active {
			continue
		}
		if source, ok := owner.(approvalObserver); ok {
			for _, v := range source.Snapshot().Approvals {
				if v.Owner == "task" && v.ID == d.ID {
					if target != nil {
						w.mu.Unlock()
						return true, errors.New("ambiguous original approval owner")
					}
					target = source
					break
				}
			}
		}
	}
	w.mu.Unlock()
	if target == nil {
		return false, nil
	}
	return true, target.Decide(ctx, d)
}

func (a *Application) allApprovalSnapshot() api.Snapshot {
	v := a.currentEngine().Snapshot()
	if a.localWork != nil {
		v.Approvals = append(v.Approvals, a.localWork.RetainedApprovals()...)
	}
	return v
}

func (w *localWorkers) RetainedApprovalURL(id string) (string, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var target api.ApprovalNavigator
	for runtime, owner := range w.owners {
		if runtime == w.active {
			continue
		}
		source, ok := owner.(approvalObserver)
		if !ok {
			continue
		}
		for _, approval := range source.Snapshot().Approvals {
			if approval.ID != id || approval.Owner != "task" {
				continue
			}
			if target != nil {
				return "", true, errors.New("ambiguous original approval owner")
			}
			target, ok = owner.(api.ApprovalNavigator)
			if !ok {
				return "", true, errors.New("original approval does not support navigation")
			}
		}
	}
	if target == nil {
		return "", false, nil
	}
	url, err := target.ApprovalURL(id)
	return url, true, err
}
