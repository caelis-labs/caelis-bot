package desktop

import (
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"path/filepath"
	"testing"
)

func TestSSHDefaultEditorUsesOnlyResolvedConfigurationPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Bot Data", "SSH", "config")
	calls := 0
	s := &Service{prepareSSHConfiguration: func(kind string) (api.SSHConfigurationDocument, error) {
		if kind != "bot" {
			return api.SSHConfigurationDocument{}, errors.New("ssh_config_scope_invalid")
		}
		return api.SSHConfigurationDocument{Path: path, Exists: true}, nil
	}, openConfigurationFile: func(got string) error {
		calls++
		if got != path {
			t.Fatal(got)
		}
		return nil
	}}
	if e := s.EditSSHConfiguration("bot"); e != nil {
		t.Fatal(e)
	}
	if e := s.EditSSHConfiguration("/etc/passwd"); e == nil || calls != 1 {
		t.Fatal("arbitrary path reached editor", e, calls)
	}
	s.openConfigurationFile = nil
	if e := s.EditSSHConfiguration("bot"); e == nil {
		t.Fatal("headless pretended to open editor")
	}
}
