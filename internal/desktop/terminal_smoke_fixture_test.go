package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
)

func TestTerminalSmokeFixtureUsesDisposableNativeLocalBinding(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "synthetic-client")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$@\" > \"$FIXTURE_RESULT\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	target := terminalSmokeTarget(directory, binary)
	if target.Target.Validate() != nil || target.Target.Role != api.RoleWorker || target.Target.Backend != target.Runtime || target.Locality != api.TerminalLocal || target.Generation == "" || target.Directory != directory {
		t.Fatal("acceptance fixture lost its explicit native binding")
	}
	if target.CodexHome != "" || target.Store != "" || target.TokenFile != "" || target.Session != "" {
		t.Fatal("synthetic terminal fixture acquired user runtime metadata")
	}
	script, err := taskterminal.Script(target)
	if err != nil {
		t.Fatal("existing acceptance fixture rejected by production attach contract", err)
	}
	result := filepath.Join(directory, "result")
	cmd := exec.CommandContext(t.Context(), "/bin/sh")
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "FIXTURE_RESULT=" + result}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal("disposable acceptance client did not execute", err, string(output))
	}
	got, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	want := directory + "\n--remote\n" + target.Endpoint + "\nresume\nsynthetic\n"
	if string(got) != want {
		t.Fatalf("fixture escaped its synthetic workspace/arguments: %q", got)
	}
	if _, err := os.Stat(filepath.Join(directory, "unused-smoke.sock")); !os.IsNotExist(err) {
		t.Fatal("synthetic client created a Runtime endpoint", err)
	}
}
