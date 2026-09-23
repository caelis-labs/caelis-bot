package caelisruntime

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOfficialCLICommandsAndCredentialIsolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows runtime management is not implemented")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "caelis")
	log := filepath.Join(dir, "calls")
	t.Setenv("CAELIS_RUNTIME_TEST_LOG", log)
	t.Setenv("CAELIS_CONTROL_TOKEN", "HOST_SECRET_SENTINEL")
	t.Setenv("CAELIS_CONTROL_URL", "http://wrong-host.invalid")
	script := `#!/bin/sh
[ -z "$CAELIS_CONTROL_TOKEN$CAELIS_CONTROL_URL" ] || exit 97
printf '%s\n' "$*" >> "$CAELIS_RUNTIME_TEST_LOG"
case "$1" in
version) printf '{"version":"0.fixture"}\n';;
update) printf 'Already current\n';;
service) printf '{"state":"running"}\n';;
*) exit 98;;
esac
`
	if e := os.WriteFile(bin, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
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
	if !strings.Contains(calls, "update --store-dir "+dir+" --check") || strings.Contains(calls, "service start") || strings.Contains(calls, "--format json --check") {
		t.Fatal("CLI contract changed or shared Host replaced", calls)
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
