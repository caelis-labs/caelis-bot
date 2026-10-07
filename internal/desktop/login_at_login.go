package desktop

import "errors"

// LoginAtLoginStatus is a projection of the operating system's current
// registration and approval state. No local preference is used as authority.
type LoginAtLoginStatus struct {
	Supported  bool   `json:"supported"`
	State      string `json:"state"`
	Enabled    bool   `json:"enabled"`
	Registered bool   `json:"registered"`
}

type loginAtLoginController interface {
	Status() LoginAtLoginStatus
	Set(bool) error
	OpenSystemSettings() error
}

var errLoginAtLoginUnsupported = errors.New("login at login is unsupported on this platform")

func (s *Service) LoginAtLoginStatus() LoginAtLoginStatus {
	if s.loginAtLogin == nil {
		return LoginAtLoginStatus{State: "unsupported"}
	}
	return s.loginAtLogin.Status()
}

func (s *Service) SetLoginAtLogin(enabled bool) (LoginAtLoginStatus, error) {
	s.loginAtLoginMu.Lock()
	defer s.loginAtLoginMu.Unlock()
	if s.loginAtLogin == nil {
		return s.LoginAtLoginStatus(), errLoginAtLoginUnsupported
	}
	before := s.loginAtLogin.Status()
	if !before.Supported {
		return before, errLoginAtLoginUnsupported
	}
	if enabled && (before.Enabled || before.State == "needsApproval") ||
		!enabled && !before.Registered {
		return before, nil
	}
	if err := s.loginAtLogin.Set(enabled); err != nil {
		return s.loginAtLogin.Status(), err
	}
	return s.loginAtLogin.Status(), nil
}

func (s *Service) OpenLoginItemsSettings() error {
	if s.loginAtLogin == nil || !s.loginAtLogin.Status().Supported {
		return errLoginAtLoginUnsupported
	}
	return s.loginAtLogin.OpenSystemSettings()
}
