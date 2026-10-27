package desktop

import (
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) PickSSHIdentity() (api.SSHIdentity, error) {
	if s.pickSSHIdentity == nil || s.registerSSHIdentity == nil {
		return api.SSHIdentity{}, errors.New("ssh_key_picker_unavailable")
	}
	path, err := s.pickSSHIdentity()
	if err != nil {
		return api.SSHIdentity{}, errors.New("ssh_key_unavailable")
	}
	if path == "" {
		return api.SSHIdentity{}, nil
	}
	return s.registerSSHIdentity(path)
}
