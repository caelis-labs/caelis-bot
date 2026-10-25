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
func (s *Service) BeginWorkerPairing() (api.WorkerSharing, error) {
	p, ok := s.workerSharing.(api.WorkerPairingStarter)
	if !ok {
		return api.WorkerSharing{}, errors.New("worker_sharing_unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return p.BeginWorkerPairing(ctx)
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
	value, err := p.SetWorkerEndpoint(in)
	if err != nil {
		var rejected *api.ActionRejectedError
		if errors.As(err, &rejected) {
			return value, err
		}
		// These fixed App errors are emitted only before bind or persistence.
		// Bind/save failures can have uncertain effects and stay unclassified.
		switch err.Error() {
		case "worker_listener_invalid", "worker_reachable_address_required", "invalid_bot_address", "worker_listener_managed_headless":
			return value, &api.ActionRejectedError{Code: err.Error()}
		}
	}
	return value, err
}

func (s *Service) ReadBotConnectionReceipt(ctx context.Context, ref api.BotConnectionReference) (api.Machine, error) {
	p, ok := s.machines.(api.BotConnectionReceiptReader)
	if !ok {
		return api.Machine{}, errors.New("bot_connection_receipt_unavailable")
	}
	return p.ReadBotConnectionReceipt(ctx, ref)
}

func (s *Service) AuthorizeBotConnection(in api.BotConnectionAuthorization) (api.Machine, error) {
	p, ok := s.machines.(api.BotConnectionAuthorizer)
	if !ok {
		return api.Machine{}, errors.New("bot_connections_unavailable")
	}
	ref, err := p.BotAuthorizationReference(in.Connection.ID)
	if err != nil {
		return api.Machine{}, err
	}
	v, _, err := AuthorizeOriginalBotConnection(s, ref, in.Connection)
	return v, err
}

// Host-only reference comes from the original authenticated Web receipt.
func AuthorizeOriginalBotConnection(s *Service, ref api.BotConnectionReference, in api.BotConnectionInput) (api.Machine, api.BotConnectionReference, error) {
	p, ok := s.machines.(api.BotConnectionAuthorizer)
	if !ok {
		return api.Machine{}, ref, errors.New("bot_connections_unavailable")
	}
	ctx, cancel := s.machineContext()
	defer cancel()
	return p.AuthorizeBotConnection(ctx, ref, in)
}
func ValidateBotAuthorizationCode(s *Service, ref api.BotConnectionReference, code string) error {
	p, ok := s.machines.(api.BotConnectionAuthorizer)
	if !ok {
		return errors.New("bot_connections_unavailable")
	}
	return p.ValidateBotAuthorizationCode(ref, code)
}

func BotAuthorizationReference(s *Service, id string) (api.BotConnectionReference, error) {
	p, ok := s.machines.(api.BotConnectionAuthorizer)
	if !ok {
		return api.BotConnectionReference{}, errors.New("bot_connections_unavailable")
	}
	return p.BotAuthorizationReference(id)
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
