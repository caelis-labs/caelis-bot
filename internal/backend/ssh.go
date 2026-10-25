package backend

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"time"
)

func (s *Service) sshConnections() (api.SSHConnections, error) {
	v, ok := s.machines.(api.SSHConnections)
	if !ok {
		return nil, errors.New("ssh_unavailable")
	}
	return v, nil
}
func (s *Service) SSHConfiguration() (api.SSHConfiguration, error) {
	v, e := s.sshConnections()
	if e != nil {
		return api.SSHConfiguration{}, e
	}
	return v.SSHConfiguration()
}
func (s *Service) SaveSSHConfiguration(in api.SSHConfigurationSave) (api.SSHConfiguration, error) {
	v, e := s.sshConnections()
	if e != nil {
		return api.SSHConfiguration{}, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return v.SaveSSHConfiguration(ctx, in)
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
func (s *Service) ReadSSHConfiguration(kind string) (api.SSHConfigurationDocument, error) {
	p, ok := s.machines.(api.SSHConfigurationFiles)
	if !ok {
		return api.SSHConfigurationDocument{}, errors.New("ssh_unavailable")
	}
	return p.ReadSSHConfiguration(kind)
}
func (s *Service) PrepareSSHConfigurationEdit(kind string) (api.SSHConfigurationDocument, error) {
	p, ok := s.machines.(api.SSHConfigurationFiles)
	if !ok {
		return api.SSHConfigurationDocument{}, errors.New("ssh_unavailable")
	}
	return p.PrepareSSHConfigurationEdit(kind)
}
