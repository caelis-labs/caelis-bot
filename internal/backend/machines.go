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
	return context.WithTimeout(context.Background(), 60*time.Second)
}
func (s *Service) ConnectMachine(v api.MachineInput) (api.Machine, error) {
	if s.machines == nil {
		return api.Machine{}, errors.New("machines unavailable")
	}
	ctx, c := s.machineContext()
	defer c()
	return s.machines.ConnectMachine(ctx, v)
}
func (s *Service) InspectMachine(id, runtime string) (api.Machine, error) {
	ctx, c := s.machineContext()
	defer c()
	return s.machines.InspectMachine(ctx, id, runtime)
}
func (s *Service) SaveMachineModel(v api.MachineModel) (api.Machine, error) {
	ctx, c := s.machineContext()
	defer c()
	return s.machines.SaveMachineModel(ctx, v)
}
func (s *Service) ChangeMachineTeam(v api.MachineTeamChange) (api.RuntimeMutationResult, error) {
	ctx, c := s.machineContext()
	defer c()
	return s.machines.ChangeMachineTeam(ctx, v)
}
func (s *Service) RemoveMachine(id string) error {
	ctx, c := s.machineContext()
	defer c()
	return s.machines.RemoveMachine(ctx, id)
}
func (s *Service) ReadMachineAdvanced(id string) (api.Machine, error) {
	ctx, c := s.machineContext()
	defer c()
	return s.machines.ReadMachineAdvanced(ctx, id)
}
