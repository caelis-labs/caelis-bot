package desktop

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestSSHKeyPickerRegistersOnlyExplicitSelection(t *testing.T) {
	count := 0
	s := &Service{pickSSHIdentity: func() (string, error) { return "", nil }, registerSSHIdentity: func(path string) (api.SSHIdentity, error) {
		count++
		return api.SSHIdentity{ID: "opaque", Name: "id_ed25519"}, nil
	}}
	if key, e := s.PickSSHIdentity(); e != nil || key.ID != "" || count != 0 {
		t.Fatal(key, e, count)
	}
	s.pickSSHIdentity = func() (string, error) { return "/fixture/key", nil }
	if key, e := s.PickSSHIdentity(); e != nil || key.ID != "opaque" || count != 1 {
		t.Fatal(key, e, count)
	}
}
