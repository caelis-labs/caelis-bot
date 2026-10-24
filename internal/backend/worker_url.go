package backend

import (
	"context"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) workerURLTask(id string) (api.WorkURLChallenge, string, error) {
	owner := s.workerInteractionOwner()
	locator, ok := owner.(interface{ WorkerApprovalTaskID(string) (string, error) })
	if !ok {
		return nil, "", errors.New("worker_url_page_unsupported")
	}
	task, err := locator.WorkerApprovalTaskID(id)
	if err != nil {
		return nil, "", err
	}
	p, ok := owner.(api.WorkURLChallenge)
	if !ok {
		return nil, "", errors.New("worker_url_page_unsupported")
	}
	return p, task, nil
}

// These methods are user UI ports only. They are not registered as Agent/MCP
// tools. The renderer supplies an original request reference, never a target URL.
func (s *Service) ApprovalPage(ctx context.Context, id string) (api.WorkURLPage, error) {
	p, task, err := s.workerURLTask(id)
	if err != nil {
		return api.WorkURLPage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return p.ReadWorkURL(ctx, task, id)
}
func (s *Service) SubmitApprovalPage(ctx context.Context, id, request, revision string, values map[string]string) (api.WorkURLReceipt, error) {
	p, task, err := s.workerURLTask(id)
	if err != nil {
		return api.WorkURLReceipt{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	op, err := p.SubmitWorkURL(ctx, api.WorkURLSubmission{TaskID: task, ApprovalID: id, RequestID: request, Revision: revision, Values: values})
	return urlReceipt(op), err
}
func (s *Service) ApprovalPageOperation(ctx context.Context, id, request string) (api.WorkURLReceipt, error) {
	p, task, err := s.workerURLTask(id)
	if err != nil {
		return api.WorkURLReceipt{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	op, err := p.ReadWorkURLSubmission(ctx, task, id, request)
	return urlReceipt(op), err
}
func urlReceipt(op api.WorkOperation) api.WorkURLReceipt {
	return api.WorkURLReceipt{RequestID: op.RequestID, Fingerprint: op.Fingerprint, Outcome: op.Outcome}
}
