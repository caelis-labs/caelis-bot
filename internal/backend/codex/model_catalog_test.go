//go:build darwin || linux

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"
)

func TestSetupAndResidentReadTheSameSharedCatalog(t *testing.T) {
	home := recoveryDirectory(t)
	socket := controlSocket(home)
	t.Setenv("CODEX_HOME", t.TempDir()) // The selected CLI reports a different home.
	fixture := &sessionFixture{modelPages: map[string]any{"": map[string]any{"data": []any{catalogEntry()}}}}
	stop := serveRecoverySocket(t, socket, fixture, home)
	t.Cleanup(stop)
	setup := setupFixture(t, func(_ *Setup, request wireMessage, conn net.Conn) {
		if request.Method != "account/read" {
			t.Errorf("management client received %s", request.Method)
		}
		writeWire(t, conn, wireMessage{ID: request.ID, Result: json.RawMessage(`{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`)})
	})
	management := setup.start
	dials := 0
	setup.start = func(ctx context.Context, opts Options) (*Client, error) {
		if opts.CLIOnly {
			client, err := management(ctx, opts)
			client.home = home
			return client, err
		}
		dials++
		if opts.Socket != socket || !opts.RequiredSocket || !opts.RecoverShared || opts.HandleRequests {
			t.Fatal("catalog dial did not require the selected home's public endpoint")
		}
		return Start(ctx, opts)
	}
	const binary = "/missing/fixture-cli"
	inspect := func(resident *Session) {
		t.Helper()
		v, err := setup.Inspect(t.Context(), binary, resident)
		if err != nil || v.State != "ready" || len(v.Models) != 1 || v.Models[0].Parameters.Model != "test-model" {
			t.Fatalf("shared catalog missing: %v %v", v.Models, err)
		}
	}
	inspect(nil)
	public, err := Start(t.Context(), Options{Socket: socket, RequiredSocket: true})
	if err != nil {
		t.Fatal(err)
	}
	defer public.Close()
	s := NewSession(SessionOptions{Directory: home, StateFile: filepath.Join(home, "binding")})
	s.client, s.state.Connection = public, "ready"
	inspect(s)
	models, err := s.Models(t.Context())
	if err != nil || len(models) != 1 || models[0].Model != "test-model" || dials != 1 {
		t.Fatalf("resident catalog not reused: %v, %v, dials=%d", models, err, dials)
	}
	// A resident attached to another home's daemon must never supply this
	// profile's list, even if that resident is otherwise ready.
	otherHome := recoveryDirectory(t)
	otherSocket := controlSocket(otherHome)
	t.Cleanup(serveRecoverySocket(t, otherSocket, &sessionFixture{}, otherHome))
	other, err := Start(t.Context(), Options{Socket: otherSocket, RequiredSocket: true})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	s.client = other
	inspect(s)
	if dials != 2 {
		t.Fatal("settings did not open its own home's public endpoint")
	}
	// Even with a warm private client, a failed catalog read cannot authorize
	// an empty list, fall back to its old models or replace a preference.
	setup.start = func(context.Context, Options) (*Client, error) { return nil, errors.New("unavailable") }
	v, err := setup.Inspect(t.Context(), binary, nil)
	if !errors.Is(err, errModelCatalog) || v.State == "ready" || len(v.Models) != 0 {
		t.Fatalf("failed read reported success: %v, %v", v.State, err)
	}
}
