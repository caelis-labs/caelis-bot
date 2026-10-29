package machines

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

type inert struct{}

func (inert) WorkAdmission(context.Context) error { return nil }
func (inert) WorkStates() []api.WorkState         { return nil }
func (inert) StartWork(context.Context, api.WorkStart) (api.Task, error) {
	panic("must not start local work")
}
func (inert) ReadWork(context.Context, string) (api.Task, error)          { return api.Task{}, nil }
func (inert) SendWork(context.Context, api.TaskMessage) (api.Task, error) { return api.Task{}, nil }
func (inert) StopWork(context.Context, string) (api.Task, error)          { return api.Task{}, nil }

type reviewReports struct{}

func (reviewReports) SubmitSystem(context.Context, api.SystemMessage) (api.Receipt, error) {
	return api.Receipt{}, nil
}

func approveMachine(t *testing.T, target *dualPeerTarget, s *Service, id string) {
	t.Helper()
	p, ok := s.machineProfile(id)
	if !ok {
		t.Fatal("missing pending machine")
	}
	if err := target.peer.DecidePairing(t.Context(), api.WorkerPairingDecision{ID: p.PairingEpoch, Allow: true}); err != nil {
		t.Fatal(err)
	}
	v, err := s.InspectMachine(t.Context(), id, "")
	if err != nil || v.State != "ready" {
		t.Fatal("approved peer not ready", v, err)
	}
}

func (reviewReports) SystemReceipt(id string) api.Receipt {
	return api.Receipt{ID: id, Outcome: "unknown"}
}
