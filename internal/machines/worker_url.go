package machines

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

func (s *Service) WorkerApprovalTaskID(id string) (string, error) {
	if ref, ok := unscopedRef("worker-approval:", id); ok && s.route(ref.Task) != "" {
		return ref.Task, nil
	}
	if p, ok := s.local.(interface{ WorkerApprovalTaskID(string) (string, error) }); ok {
		return p.WorkerApprovalTaskID(id)
	}
	return "", errors.New("original Worker request unavailable")
}
func nativeURLReference(task, id string) (string, error) {
	if ref, ok := unscopedRef("worker-approval:", id); ok {
		if ref.Task != task {
			return "", errors.New("worker_request_source_conflict")
		}
		return ref.Native, nil
	}
	return id, nil
}
func (s *Service) ReadWorkURL(ctx context.Context, task, id string) (api.WorkURLPage, error) {
	if s.route(task) == "" {
		if p, ok := s.local.(api.WorkURLChallenge); ok {
			return p.ReadWorkURL(ctx, task, id)
		}
		return api.WorkURLPage{}, errors.New("worker_url_page_unsupported")
	}
	c, runtime, err := s.connection(task)
	if err != nil {
		return api.WorkURLPage{}, err
	}
	id, err = nativeURLReference(task, id)
	if err != nil {
		return api.WorkURLPage{}, err
	}
	out, err := s.peerCall(ctx, c, workerpeer.Request{Action: "url-page", Runtime: runtime, ID: task, Ref: id})
	return out.URLPage, err
}
func (s *Service) SubmitWorkURL(ctx context.Context, in api.WorkURLSubmission) (api.WorkOperation, error) {
	if s.route(in.TaskID) == "" {
		if p, ok := s.local.(api.WorkURLChallenge); ok {
			return p.SubmitWorkURL(ctx, in)
		}
		return api.WorkOperation{}, errors.New("worker_url_page_unsupported")
	}
	c, runtime, err := s.connection(in.TaskID)
	if err != nil {
		return api.WorkOperation{}, err
	}
	in.ApprovalID, err = nativeURLReference(in.TaskID, in.ApprovalID)
	if err != nil {
		return api.WorkOperation{}, err
	}
	out, err := s.peerCall(ctx, c, workerpeer.Request{Action: "url-submit", Runtime: runtime, URLSubmission: in})
	return out.Operation, err
}
func (s *Service) ReadWorkURLSubmission(ctx context.Context, task, id, request string) (api.WorkOperation, error) {
	if s.route(task) == "" {
		if p, ok := s.local.(api.WorkURLChallenge); ok {
			return p.ReadWorkURLSubmission(ctx, task, id, request)
		}
		return api.WorkOperation{}, errors.New("worker_url_page_unsupported")
	}
	c, runtime, err := s.connection(task)
	if err != nil {
		return api.WorkOperation{}, err
	}
	id, err = nativeURLReference(task, id)
	if err != nil {
		return api.WorkOperation{}, err
	}
	out, err := s.peerCall(ctx, c, workerpeer.Request{Action: "url-operation", Runtime: runtime, ID: task, Ref: id, Query: api.WorkOperationQuery{RequestID: request}})
	return out.Operation, err
}
