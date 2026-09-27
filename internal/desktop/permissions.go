package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

type SystemPermission struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Target string `json:"target,omitempty"`
}
type SystemPermissionState struct {
	Supported   bool               `json:"supported"`
	AppPath     string             `json:"appPath"`
	Development bool               `json:"development"`
	Permissions []SystemPermission `json:"permissions"`
}
type permissionGuide struct {
	mu   sync.Mutex
	file string
	Seen bool `json:"seen"`
}

func (s *Service) configurePermissionGuide(file string) {
	s.permissionGuide.file = file
	data, err := os.ReadFile(file)
	if err == nil {
		_ = json.Unmarshal(data, &s.permissionGuide)
	}
}
func (s *Service) PermissionGuidePending() bool {
	s.permissionGuide.mu.Lock()
	defer s.permissionGuide.mu.Unlock()
	return !s.permissionGuide.Seen
}
func (s *Service) FinishPermissionGuide() error {
	s.permissionGuide.mu.Lock()
	defer s.permissionGuide.mu.Unlock()
	if err := localstate.Write(s.permissionGuide.file, struct {
		Seen bool `json:"seen"`
	}{true}); err != nil {
		return err
	}
	s.permissionGuide.Seen = true
	return nil
}
func (s *Service) OpenSystemPermissions() { s.showSettings("permissions") }
func (s *Service) SystemPermissions() SystemPermissionState {
	state := systemPermissionState(s.permissionTerminal())
	if state.Supported {
		state.Permissions = append(state.Permissions, SystemPermission{ID: "notifications", Status: s.NotificationStatus()})
	}
	return state
}
func (s *Service) permissionTerminal() string {
	if s.taskPreferences == nil {
		return "system"
	}
	return s.taskPreferences().Terminal
}

// Legacy bridge entry delegates to the same request path.
func (s *Service) ConfigureSystemPermission(ctx context.Context, id string) error {
	_, err := s.RequestSystemPermission(ctx, id)
	return err
}
func (s *Service) RequestSystemPermission(ctx context.Context, id string) (PermissionRequestResult, error) {
	if err := ctx.Err(); err != nil {
		return PermissionRequestResult{}, err
	}
	if id == "notifications" {
		status := s.NotificationStatus()
		if status == "unavailable" {
			return PermissionRequestResult{}, errors.New("notifications are unavailable")
		}
		s.ConfigureNotifications()
		if status == "notDetermined" {
			return PermissionRequestResult{State: "requested"}, nil
		}
		return PermissionRequestResult{State: "settingsOpened"}, nil
	}
	return requestSystemPermission(ctx, id, s.permissionTerminal())
}

// Disabling a switch goes to macOS. It never resets TCC or impersonates an OS grant.
func (s *Service) OpenSystemPermissionSettings(id string) error {
	return openSystemPermissionSettings(id)
}

// Reset is a separate, explicitly confirmed Settings action. Never call it from
// startup, a permission read, or an ordinary enable action. The OS cannot select
// an old version: this revokes the category for all copies of this bundle ID.
func (s *Service) ResetSystemPermission(ctx context.Context, id string) error {
	service := permissionResetService(id)
	if service == "" {
		return errors.New("unsupported permission reset")
	}
	return resetSystemPermission(ctx, service)
}
func permissionResetService(id string) string {
	switch id {
	case "accessibility":
		return "Accessibility"
	case "screenCapture":
		return "ScreenCapture"
	case "automation":
		return "AppleEvents"
	}
	return ""
}
func (s *Service) RevealPermissionApp() error { return revealPermissionApp() }
