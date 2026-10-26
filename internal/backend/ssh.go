package backend

import (
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) sshConnections() (api.SSHConnections, error) {
	v, ok := s.machines.(api.SSHConnections)
	if !ok {
		return nil, errors.New("ssh_unavailable")
	}
	return v, nil
}
func (s *Service) SSHHosts() (api.SSHHosts, error) {
	v, e := s.sshConnections()
	if e != nil {
		return api.SSHHosts{}, e
	}
	return v.SSHHosts()
}
func (s *Service) RegisterSSHIdentity(path string) (api.SSHIdentity, error) {
	v, e := s.sshConnections()
	if e != nil {
		return api.SSHIdentity{}, e
	}
	return v.RegisterSSHIdentity(path)
}
func (s *Service) StartSSHConnection(in api.SSHTarget) (api.SSHConnectionStatus, error) {
	v, e := s.sshConnections()
	if e != nil {
		return api.SSHConnectionStatus{}, e
	}
	return v.StartSSHConnection(in)
}
func (s *Service) SSHConnectionStatus(in api.SSHTarget) api.SSHConnectionStatus {
	v, e := s.sshConnections()
	if e != nil {
		return api.SSHConnectionStatus{State: "failed", Issue: "ssh_unavailable"}
	}
	return v.SSHConnectionStatus(in)
}
func (s *Service) AnswerSSH(in api.SSHAnswer) error {
	v, e := s.sshConnections()
	if e != nil {
		return e
	}
	return v.AnswerSSH(in)
}
func (s *Service) StopSSHConnection(in api.SSHTarget) error {
	v, e := s.sshConnections()
	if e != nil {
		return e
	}
	return v.StopSSHConnection(in)
}
