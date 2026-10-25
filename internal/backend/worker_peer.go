package backend

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"time"
)

func (s *Service) ConfigureWorkerSharing(v api.WorkerSharingController) { s.workerSharing = v }
func (s *Service) ConfigureWorkerInteractions(v any) {
	s.workerInteractionMu.Lock()
	s.workerInteractions = v
	s.workerInteractionMu.Unlock()
}
func (s *Service) workerInteractionOwner() any {
	s.workerInteractionMu.RLock()
	defer s.workerInteractionMu.RUnlock()
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

func (s *Service) ReadBotConnectionReceipt(ctx context.Context, ref api.BotConnectionReference) (api.Machine, error) {
	p, ok := s.machines.(api.BotConnectionReceiptReader)
	if !ok {
		return api.Machine{}, errors.New("bot_connection_receipt_unavailable")
	}
	return p.ReadBotConnectionReceipt(ctx, ref)
}

func PrepareBotConnection(s *Service, in api.BotConnectionInput, ref *api.BotConnectionReference) (api.BotConnectionReference, error) {
	p, ok := s.machines.(api.BotConnectionLifecycle)
	if !ok {
		return api.BotConnectionReference{}, errors.New("bot_connections_unavailable")
	}
	return p.PrepareBotConnection(in, ref)
}
func ConnectPreparedBot(s *Service, ref api.BotConnectionReference, in api.BotConnectionInput) (api.Machine, api.BotConnectionReference, error) {
	p, ok := s.machines.(api.BotConnectionLifecycle)
	if !ok {
		return api.Machine{}, ref, errors.New("bot_connections_unavailable")
	}
	ctx, cancel := s.machineContext()
	defer cancel()
	return p.ConnectPreparedBot(ctx, ref, in)
}
func BotConnectionQuiescent(s *Service, ref api.BotConnectionReference) bool {
	p, ok := s.machines.(api.BotConnectionLifecycle)
	return ok && p.BotConnectionQuiescent(ref)
}
func AbortPreparedBotConnection(s *Service, ref api.BotConnectionReference) error {
	p, ok := s.machines.(api.BotConnectionLifecycle)
	if !ok {
		return errors.New("bot_connections_unavailable")
	}
	return p.AbortPreparedBotConnection(ref)
}

// Resolve the existing owner, not a browser-supplied prefix or boolean.
func RemoteWorkApproval(s *Service, id string) bool {
	p, ok := s.workerInteractionOwner().(interface{ RemoteWorkApproval(string) bool })
	return ok && p.RemoteWorkApproval(id)
}

func (s *Service) DecideWorkerPairing(in api.WorkerPairingDecision) error {
	p, ok := s.workerSharing.(api.WorkerPairingRequests)
	if !ok {
		return errors.New("worker_pairing_unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return p.DecideWorkerPairing(ctx, in)
}
