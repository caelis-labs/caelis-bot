//go:build windows

package caelis

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
)

// Real published CLI + shared Host lifecycle, entirely in an isolated Store.
// No model call, user credential or existing Host is needed or modified.
func TestWindowsNativeHostDiscoveryAndLifecycle(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set verified native Caelis binary")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	packageDir := filepath.Join(root, "ZIP")
	if err := os.Mkdir(packageDir, 0700); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	packageCLI := filepath.Join(packageDir, "caelis.exe")
	if err := os.WriteFile(packageCLI, bytes, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(packageDir)
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(t.TempDir(), "隔离 Store")}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cleanup, bin, "service", "stop", "--store-dir", settings.CaelisStore, "--format", "json")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		cmd.Env = runtimeenv.Clean(os.Environ())
		if err := cmd.Run(); err != nil {
			t.Error("isolated Host cleanup", err)
		}
	}()
	status, err := caelisruntime.Manage(ctx, "start", packageCLI, settings.CaelisStore)
	if err != nil || !status.Installed {
		t.Fatal("native service start", err)
	}
	d, token, err := Discover(settings)
	if err != nil || d.InstanceID == "" || token == "" {
		t.Fatal("native protected discovery", err)
	}
	if d.DistributionVersion != status.Version {
		t.Fatal("Host version differs from offered CLI")
	}
	if err = ProbeBinding(ctx, settings, ""); err != nil {
		t.Fatal("public Bot capability handshake", err)
	}
	// The GUI has released its CWD; the real detached Host remains alive.
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(packageDir); err != nil {
		t.Fatal("Host locks ZIP package", err)
	}
	host, err := newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, cwd string }{
		{"default", ""},
		{"Notebook", filepath.Join(root, "Notebook")},
		{"Worker", filepath.Join(root, "worker")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := "workspace-" + tc.name
			req := wire.CreateSessionRequest{OperationId: &op}
			want := filepath.Join(root, "local", "Caelis Bot", "host-workspace")
			if tc.cwd != "" {
				if err := os.Mkdir(tc.cwd, 0700); err != nil {
					t.Fatal(err)
				}
				req.Cwd = &tc.cwd
				want = tc.cwd
			}
			var result wire.CommandResult
			if err := host.json(ctx, "POST", "/sessions", req, &result, op, ""); err != nil || !succeeded(result.Outcome) || value(result.SessionId) == "" {
				t.Fatal("create workspace session", err, result.Outcome)
			}
			var state wire.SessionState
			if err := host.json(ctx, "GET", "/sessions/"+idPath(value(result.SessionId))+"/state", nil, &state, "", ""); err != nil {
				t.Fatal(err)
			}
			actualInfo, actualErr := os.Stat(value(state.Cwd))
			wantInfo, wantErr := os.Stat(want)
			if actualErr != nil || wantErr != nil || !os.SameFile(actualInfo, wantInfo) {
				t.Fatal("Host default or explicit workspace changed", value(state.Cwd))
			}
		})
	}
	if _, err = caelisruntime.Manage(ctx, "start", bin, settings.CaelisStore); err != nil {
		t.Fatal(err)
	}
	next, _, err := Discover(settings)
	if err != nil || next.InstanceID != d.InstanceID {
		t.Fatal("idempotent start replaced current Host", err)
	}
	t.Log("published CLI discovery, private credentials, Bot capabilities and idempotent service start verified")
}
