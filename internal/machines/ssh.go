package machines

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/sshtransport"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

func (s *Service) SSHConfiguration() (api.SSHConfiguration, error) { return s.ssh.Configuration() }
func (s *Service) SaveSSHConfiguration(ctx context.Context, in api.SSHConfigurationSave) (api.SSHConfiguration, error) {
	return s.ssh.Save(ctx, in)
}
func (s *Service) StartSSHConnection(v api.SSHTarget) (api.SSHConnectionStatus, error) {
	return s.ssh.Start(v)
}
func (s *Service) SSHConnectionStatus(v api.SSHTarget) api.SSHConnectionStatus {
	return s.ssh.Status(v)
}
func (s *Service) AnswerSSH(v api.SSHAnswer) error         { return s.ssh.Answer(v) }
func (s *Service) StopSSHConnection(v api.SSHTarget) error { return s.ssh.Stop(v) }
func (s *Service) peerConnection(ctx context.Context, c workerpeer.Connection) (workerpeer.Connection, error) {
	a, e := s.checkedSSHAddress(ctx, c.Address, c.Peer)
	c.Address = a
	return c, e
}
func (s *Service) peerCall(ctx context.Context, c workerpeer.Connection, in workerpeer.Request) (workerpeer.Response, error) {
	resolved, e := s.peerConnection(ctx, c)
	if e != nil {
		return workerpeer.Response{}, e
	}
	return workerpeer.Call(ctx, resolved, in)
}
func (s *Service) peerAddress(ctx context.Context, address string) (string, error) {
	return s.ssh.Resolve(ctx, address)
}
func (s *Service) peerWatch(ctx context.Context, c workerpeer.Connection, id string, receive func(api.WorkObservation)) error {
	resolved, e := s.peerConnection(ctx, c)
	if e != nil {
		return e
	}
	return workerpeer.Watch(ctx, resolved, id, receive)
}
func botEndpoint(address string) (string, error) {
	if v, ok := sshtransport.Target(address); ok {
		return sshtransport.Address(v)
	}
	return workerpeer.Address(address)
}
func (s *Service) pairRead(ctx context.Context, address, id, source, proof, peer string) (workerpeer.PairingReceipt, error) {
	a, e := s.checkedSSHAddress(ctx, address, peer)
	if e != nil {
		return workerpeer.PairingReceipt{}, e
	}
	return workerpeer.ReadPairing(ctx, a, id, source, proof)
}
func (s *Service) pairRequest(ctx context.Context, address, id, source, name, proof, peer string) (workerpeer.PairingReceipt, error) {
	a, e := s.checkedSSHAddress(ctx, address, peer)
	if e != nil {
		return workerpeer.PairingReceipt{}, e
	}
	return workerpeer.RequestPairing(ctx, a, id, source, name, proof, peer)
}
func (s *Service) peerIdentity(ctx context.Context, address string) (workerpeer.PairingReceipt, error) {
	a, e := s.peerAddress(ctx, address)
	if e != nil {
		return workerpeer.PairingReceipt{}, e
	}
	return workerpeer.PeerIdentity(ctx, a)
}

func (s *Service) Close() {
	if s.ssh != nil {
		s.ssh.Close()
	}
}

func (s *Service) checkedSSHAddress(ctx context.Context, address, peer string) (string, error) {
	a, e := s.ssh.Resolve(ctx, address)
	if e != nil {
		return "", e
	}
	if _, ssh := sshtransport.Target(address); ssh && peer != "" {
		v, e := workerpeer.PeerIdentity(ctx, a)
		if e != nil {
			return "", e
		}
		if v.Peer != peer {
			return "", errors.New("bot_peer_identity_changed")
		}
	}
	return a, nil
}
func (s *Service) ReadSSHConfiguration(kind string) (api.SSHConfigurationDocument, error) {
	return s.ssh.Document(kind)
}
func (s *Service) PrepareSSHConfigurationEdit(kind string) (api.SSHConfigurationDocument, error) {
	return s.ssh.PrepareEdit(kind)
}
