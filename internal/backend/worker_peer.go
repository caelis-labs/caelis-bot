package backend

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"time"
)

func (s *Service) ConfigureWorkerSharing(v api.WorkerSharingController) { s.workerSharing = v }
func (s *Service) ConfigureWorkerInteractions(v any)                    { s.workerInteractions = v }
func (s *Service) workerInteractionOwner() any {
	if s.workerInteractions != nil {
		return s.workerInteractions
	}
	return s.localWorkers
}
func (s *Service) WorkerSharing() api.WorkerSharing {
	if s.workerSharing == nil {
		return api.WorkerSharing{}
	}
	return s.workerSharing.WorkerSharing()
}
func (s *Service) SetWorkerSharing(enabled bool) (api.WorkerSharing, error) {
	if s.workerSharing == nil {
		return api.WorkerSharing{}, errors.New("worker_sharing_unavailable")
	}
	return s.workerSharing.SetWorkerSharing(enabled)
}
func (s *Service) CreateWorkerPairing() (api.WorkerSharing, error) {
	if s.workerSharing == nil {
		return api.WorkerSharing{}, errors.New("worker_sharing_unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.workerSharing.CreateWorkerPairing(ctx)
}
func (s *Service) RevokeWorkerPeer(peer string) error {
	if s.workerSharing == nil {
		return errors.New("worker_sharing_unavailable")
	}
	return s.workerSharing.RevokeWorkerPeer(peer)
}
func (s *Service) ConnectBot(in api.BotConnectionInput) (api.Machine, error) {
	p, ok := s.machines.(api.BotConnectionController)
	if !ok {
		return api.Machine{}, errors.New("bot_connections_unavailable")
	}
	ctx, cancel := s.machineContext()
	defer cancel()
	return p.ConnectBot(ctx, in)
}
func (s *Service) RecoverBotWork(id string) (api.Machine, error) {
	p, ok := s.machines.(api.BotWorkRecovery)
	if !ok {
		return api.Machine{}, errors.New("bot_recovery_unavailable")
	}
	ctx, cancel := s.machineContext()
	defer cancel()
	return p.RecoverBotWork(ctx, id)
}
func (s *Service) RecoverWorkerSharing() (api.WorkerSharing, error) {
	p, ok := s.workerSharing.(api.WorkerSharingRecovery)
	if !ok {
		return api.WorkerSharing{}, errors.New("worker_recovery_unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return p.RecoverWorkerSharing(ctx)
}
func (s *Service) SaveWorkerSharing(in api.WorkerSharingInput) (api.WorkerSharing, error) {
	p, ok := s.workerSharing.(api.WorkerSharingEndpoint)
	if !ok {
		return api.WorkerSharing{}, errors.New("worker_listener_configuration_unavailable")
	}
	return p.SetWorkerEndpoint(in)
}
