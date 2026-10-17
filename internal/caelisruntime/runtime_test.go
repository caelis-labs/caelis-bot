package caelisruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficialCLICommandsAndCredentialIsolation(t *testing.T) {
	dir := t.TempDir()
	bin := nativeCommandFixture(t, dir, "caelis")
	log := filepath.Join(dir, "calls")
	t.Setenv("CAELIS_RUNTIME_TEST_LOG", log)
	t.Setenv("CAELIS_CONTROL_TOKEN", "HOST_SECRET_SENTINEL")
	t.Setenv("CAELIS_CONTROL_URL", "http://wrong-host.invalid")
	t.Setenv("CODEX_THREAD_ID", "foreign-caller")
	t.Setenv("RUNTIME_TOOL_SETTING", "native-export")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	nativeCommandFixture(t, dir, "worker-tool")
	for _, action := range []string{"detect", "check-update", "update", "start"} {
		v, e := Manage(context.Background(), action, bin, dir)
		if e != nil || !v.Installed || v.Path != bin || v.Version != "0.fixture" {
			t.Fatal(action, v, e)
		}
	}
	raw, e := os.ReadFile(log)
	if e != nil {
		t.Fatal(e)
	}
	calls := string(raw)
	if !strings.Contains(calls, "update --store-dir "+dir+" --check") || !strings.Contains(calls, "service start --store-dir "+dir) || strings.Contains(calls, "--format json --check") {
		t.Fatal("CLI lifecycle convergence contract changed", calls)
	}
	if _, e = Manage(context.Background(), "install", bin, dir); e == nil {
		t.Fatal("installer replaced custom binary")
	}
	if _, e = Manage(context.Background(), "restart", bin, dir); e == nil {
		t.Fatal("exposed unapproved Host restart")
	}
}
func TestInvalidLocationDoesNotExecute(t *testing.T) {
	if _, e := Find("relative/caelis"); e == nil {
		t.Fatal("relative executable accepted")
	}
	if _, e := Store("relative/store"); e == nil {
		t.Fatal("relative store accepted")
	}
	path := filepath.Join(t.TempDir(), "not-executable")
	_ = os.WriteFile(path, []byte("no"), 0600)
	if _, e := Find(path); e == nil {
		t.Fatal("non-executable accepted")
	}
}

func TestUpdateCompletionIsStructuredAndNeverLeaksInstallerOutput(t *testing.T) {
	for _, tt := range []struct {
		output string
		check  bool
		state  string
		fail   bool
	}{
		{"update available: v0.61.0 -> v0.62.0 (raw)", true, "available", false},
		{"caelis is up to date (v0.62.0)", true, "current", false},
		{"PRIVATE_INSTALLER_OUTPUT\nCaelis v0.62.0 is installed (updated from v0.61.0 via raw); it takes effect on the next start.", false, "", false},
		{"update skipped: unsupported", false, "", true},
		{"PRIVATE_UNCONFIRMED_OUTPUT", false, "", true},
	} {
		v, err := updateResult(Status{Version: "v0.62.0"}, tt.output, tt.check)
		if (err != nil) != tt.fail || v.UpdateState != tt.state || strings.Contains(v.Message, "PRIVATE") {
			t.Fatal(v, err)
		}
	}
}

func TestOldHostReplacementShowsBoundedRestartInstruction(t *testing.T) {
	path := nativeCommandFixture(t, t.TempDir(), "caelis")
	t.Setenv("CAELIS_RUNTIME_FIXTURE_RESTART", "1")
	_, err := Manage(t.Context(), "apply-update", path, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "caelis service restart") || strings.Contains(err.Error(), "SENTINEL") {
		t.Fatalf("unsafe or missing upgrade diagnostic: %v", err)
	}
}
