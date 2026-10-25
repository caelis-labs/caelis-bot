package desktop

import (
	"errors"
)

func (s *Service) EditSSHConfiguration(kind string) error {
	if s.prepareSSHConfiguration == nil || s.openConfigurationFile == nil {
		return errors.New("ssh_editor_unavailable")
	}
	v, e := s.prepareSSHConfiguration(kind)
	if e != nil {
		return e
	}
	if s.openConfigurationFile(v.Path) != nil {
		return errors.New("ssh_editor_unavailable")
	}
	return nil
}
func (s *Service) RevealSSHConfiguration(kind string) error {
	if s.readSSHConfiguration == nil || s.revealConfigurationFile == nil {
		return errors.New("ssh_location_unavailable")
	}
	v, e := s.readSSHConfiguration(kind)
	if e != nil {
		return e
	}
	if !v.Exists {
		return errors.New("ssh_config_missing")
	}
	if s.revealConfigurationFile(v.Path) != nil {
		return errors.New("ssh_location_unavailable")
	}
	return nil
}
