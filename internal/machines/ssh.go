package machines

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/sshtransport"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"strings"
)

func (s *Service) SSHHosts() (api.SSHHosts, error) {
	v, e := s.ssh.Configuration()
	return api.SSHHosts{Hosts: v.Hosts, Issue: v.Issue}, e
}
func (s *Service) RegisterSSHIdentity(path string) (api.SSHIdentity, error) {
	return s.ssh.RegisterIdentity(path)
}
func (s *Service) StartSSHConnection(v api.SSHTarget) (api.SSHConnectionStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	targetAddress, err := sshtransport.Address(v)
	if err != nil {
		return api.SSHConnectionStatus{}, err
	}
	// Reclaim abandoned, unreferenced attempts without forgetting stopped or
	// authentication-blocked channels still owned by another connection.
	for _, address := range s.ssh.FailedChannelAddresses() {
		if address != targetAddress {
			s.releaseSSHIfUnusedLocked(address)
		}
	}
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
	release, err := s.holdPeerConnection(c)
	if err != nil {
		return workerpeer.Response{}, err
	}
	defer release()
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
	release, err := s.holdPeerConnection(c)
	if err != nil {
		return err
	}
	defer release()
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

func sshChannelAddress(address string, legacy bool) string {
	target, ok := sshtransport.Target(address)
	if !ok && legacy && address != "" && !strings.Contains(address, "://") {
		target = api.SSHTarget{Host: address, Port: 9317}
		ok = true
	}
	if !ok {
		return ""
	}
	normalized, err := sshtransport.Address(target)
	if err != nil {
		return ""
	}
	return normalized
}

// Pin an admitted network call until it finishes. Deletion prevents new calls;
// existing calls may finish without closing a shared SSH transport underneath.
func (s *Service) holdPeerConnection(c workerpeer.Connection) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.state.Profiles {
		if p.Peer == c && !p.Legacy {
			return s.holdSSHLocked(c.Address), nil
		}
	}
	return nil, errors.New("original_machine_unavailable")
}
