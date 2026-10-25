package backend

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"time"
)

func (s *Service) ConfigureMachines(v api.MachineController) { s.machines = v }
func (s *Service) Machines() []api.Machine {
	if s.machines == nil {
		return []api.Machine{}
	}
	return s.machines.Machines()
}
func (s *Service) machineContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 8*time.Second)
}
func (s *Service) InspectMachine(id, runtime string) (api.Machine, error) {
	if s.machines == nil {
		return api.Machine{}, errors.New("machines unavailable")
	}
	ctx, c := s.machineContext()
	defer c()
	return s.machines.InspectMachine(ctx, id, runtime)
}
func (s *Service) RemoveMachine(id string) error {
	if s.machines == nil {
		return errors.New("machines unavailable")
	}
	ctx, c := s.machineContext()
	defer c()
	return s.machines.RemoveMachine(ctx, id)
}
