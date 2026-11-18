//go:build darwin || linux

package codex

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestSetupAndResidentReadTheSameSharedCatalog(t *testing.T) {
	home := recoveryDirectory(t)
	t.Setenv("CODEX_HOME", home)
	socket := controlSocket(home)
	fixture := &sessionFixture{modelPages: map[string]any{"": map[string]any{"data": []any{catalogEntry()}}}}
	t.Cleanup(serveRecoverySocket(t, socket, fixture, home))
	public, err := Start(t.Context(), Options{Socket: socket, RequiredSocket: true})
	if err != nil {
		t.Fatal(err)
	}
	defer public.Close()
	resident := NewSession(SessionOptions{Directory: home, StateFile: filepath.Join(home, "binding")})
	resident.client, resident.state.Connection = public, "ready"
	setup := &Setup{}
	t.Cleanup(setup.Close)
	dials := 0
	setup.start = func(ctx context.Context, opts Options) (*Client, error) {
		dials++
		if opts.CLIOnly || opts.Socket != socket || !opts.RequiredSocket || !opts.RecoverShared || opts.HandleRequests {
			t.Fatal("settings must only attach to the public daemon")
		}
		return Start(ctx, opts)
	}
	for _, path := range []string{"/missing/cli", "/upgraded/cli"} {
		v, err := setup.Inspect(t.Context(), path)
		if err != nil || v.State != "ready" || len(v.Models) != 1 || v.Models[0].Parameters.Model != "test-model" {
			t.Fatalf("shared catalog missing: %v %v", v.Models, err)
		}
	}
	setup.Close()
	models, err := resident.Models(t.Context())
	if err != nil || len(models) != 1 || models[0].Model != "test-model" || dials != 1 {
		t.Fatalf("resident catalog after settings closed: %v, %v, dials=%d", models, err, dials)
	}
	offline := &Setup{start: func(context.Context, Options) (*Client, error) { return nil, errors.New("unavailable") }}
	defer offline.Close()
	v, err := offline.Inspect(t.Context(), "/fixture")
	if err != nil || v.State != "unavailable" || len(v.Models) != 0 {
		t.Fatalf("unavailable daemon reported as ready: %v %v", v.State, err)
	}
}
