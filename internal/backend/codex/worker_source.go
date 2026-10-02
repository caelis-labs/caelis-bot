package codex

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (*WorkerClient) LeaseAwareAdmission() bool { return false }
func (*WorkerClient) checkWorkerLease(_ context.Context, s api.WorkDispatchSource) error {
	if s.Lease != (api.WorkerLeaseGrant{}) {
		return errors.New("Worker lease admission unavailable")
	}
	return nil
}
func (*WorkerClient) beginControl(ctx context.Context, _ string) (context.Context, func(), error) {
	return ctx, func() {}, nil
}
