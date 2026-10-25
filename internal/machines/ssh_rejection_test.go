package machines

import (
	"errors"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestSSHKnownRejectionDoesNotDispatchOrReplaceBinding(t *testing.T) {
	s, err := Open(t.TempDir(), inert{}, func(string) ([]byte, error) {
		t.Fatal("rejected connection reached helper installation")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s.state.Profiles["machine-ABCDEFGHIJKLMNOPQRSTUVWXYZ"] = profile{View: api.Machine{ID: "machine-ABCDEFGHIJKLMNOPQRSTUVWXYZ", Address: "old.example", Port: 22, User: "fixture", Authentication: "agent"}, Identity: "original"}
	s.state.Routes["old-task"] = "machine-ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	s.state.Profiles["machine-BCDEFGHIJKLMNOPQRSTUVWXYZ2"] = profile{View: api.Machine{ID: "machine-BCDEFGHIJKLMNOPQRSTUVWXYZ2", ConnectionType: "bot", Address: "http://original"}}
	for _, tc := range []struct {
		input api.MachineInput
		code  string
	}{
		{api.MachineInput{Name: strings.Repeat("x", 81)}, "invalid_name"},
		{api.MachineInput{ID: "machine-ABCDEFGHIJKLMNOPQRSTUVWXYZ", Address: "new.example", Port: 22, User: "fixture", Authentication: "agent"}, "machine_has_tasks"},
		{api.MachineInput{ID: "machine-BCDEFGHIJKLMNOPQRSTUVWXYZ2", Address: "new.example", Port: 22, User: "fixture", Authentication: "agent"}, "machine_connection_type_conflict"},
	} {
		_, err = s.ConnectMachine(t.Context(), tc.input)
		var rejected *api.ActionRejectedError
		if !errors.As(err, &rejected) || rejected.Code != tc.code {
			t.Fatalf("%s: %v", tc.code, err)
		}
	}
	if s.state.Routes["old-task"] != "machine-ABCDEFGHIJKLMNOPQRSTUVWXYZ" || s.state.Profiles["machine-ABCDEFGHIJKLMNOPQRSTUVWXYZ"].Identity != "original" || s.state.Profiles["machine-BCDEFGHIJKLMNOPQRSTUVWXYZ2"].View.ConnectionType != "bot" {
		t.Fatal("known rejection changed original binding")
	}
}
