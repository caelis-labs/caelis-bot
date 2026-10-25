package websurface

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/machines"
	"strings"
	"testing"
)

func TestSSHActualManagerRefusalIsTerminalAndRemoteUnknownDoesNotGateLocalUpdate(t *testing.T) {
	s := testServer(t)
	c := pairClient(t, s)
	m, err := machines.Open(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.services.Backend.ConfigureMachines(m)
	body := map[string]any{"id": "original-ssh-invalid-name", "args": []any{api.MachineInput{ID: "machine-original", Name: strings.Repeat("x", 81)}}}
	w := c.request("/api/v1/machines/connect-machine", body)
	op := operationResponse(t, s, c, "original-ssh-invalid-name")
	if w.Code != 409 || op["error"] != "operation_rejected" || op["detail"] != "invalid_name" {
		t.Fatal(w.Body.String(), op)
	}
	// A truly unknown remote write remains unknown, but never an app-wide gate.
	s.register(action{Path: "/api/test/ssh-unknown", Remote: true, Function: func(context.Context) error { return context.DeadlineExceeded }})
	w = c.request("/api/test/ssh-unknown", map[string]any{"id": "original-ssh-unknown", "args": []any{}})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "operation_unconfirmed") || !s.UpdateQuiescent() {
		t.Fatal("remote uncertainty poisoned local updates", w.Body.String())
	}
}
