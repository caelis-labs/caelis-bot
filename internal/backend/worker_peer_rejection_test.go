package backend

import (
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type endpointRejectionFixture struct {
	api.WorkerSharingController
	err error
}

func (f endpointRejectionFixture) SetWorkerEndpoint(api.WorkerSharingInput) (api.WorkerSharing, error) {
	return api.WorkerSharing{}, f.err
}

func TestWorkerEndpointOnlyKnownPreDispatchValidationIsRejected(t *testing.T) {
	for _, code := range []string{"worker_listener_invalid", "worker_reachable_address_required", "invalid_bot_address", "worker_listener_managed_headless", "worker_listener_in_use", "worker_listener_unavailable", "save failed after write", "worker_listener_invalid: private details"} {
		t.Run(code, func(t *testing.T) {
			s := NewService(nil, nil, nil, nil, nil)
			s.ConfigureWorkerSharing(endpointRejectionFixture{err: errors.New(code)})
			_, err := s.SaveWorkerSharing(api.WorkerSharingInput{})
			var rejected *api.ActionRejectedError
			known := code == "worker_listener_invalid" || code == "worker_reachable_address_required" || code == "invalid_bot_address" || code == "worker_listener_managed_headless"
			if errors.As(err, &rejected) != known || known && rejected.Code != code {
				t.Fatal("bind/save ambiguity was classified as a known input refusal", err)
			}
		})
	}
}
