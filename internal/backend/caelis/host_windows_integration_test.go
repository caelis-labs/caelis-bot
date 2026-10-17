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
	status, err := caelisruntime.Manage(ctx, "start", bin, settings.CaelisStore)
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
	if _, err = caelisruntime.Manage(ctx, "start", bin, settings.CaelisStore); err != nil {
		t.Fatal(err)
	}
	next, _, err := Discover(settings)
	if err != nil || next.InstanceID != d.InstanceID {
		t.Fatal("idempotent start replaced current Host", err)
	}
	t.Log("published CLI discovery, private credentials, Bot capabilities and idempotent service start verified")
}
