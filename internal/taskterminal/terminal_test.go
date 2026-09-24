package taskterminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestLaunchPreservesArgumentsAndReusesPrivateFile(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "work ' $(touch unexpected)")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "codex ' $(touch unexpected)")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE_ARGS\"\nprintf '%s\\n' \"$PWD\" \"$CODEX_HOME\" > \"$CAPTURE_ENV\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	target := api.TerminalTarget{Binary: binary, Endpoint: "unix:///tmp/runtime ' $(touch unexpected).sock", Thread: "owned-worker-1", Directory: workspace, CodexHome: filepath.Join(dir, "home ' $(touch unexpected)")}
	var paths []string
	l := New(filepath.Join(dir, "launch"), func(ctx context.Context, path string) error {
		paths = append(paths, path)
		cmd := exec.CommandContext(ctx, "/bin/sh", path)
		cmd.Env = append(os.Environ(), "CAPTURE_ARGS="+filepath.Join(dir, "args"), "CAPTURE_ENV="+filepath.Join(dir, "env"))
		return cmd.Run()
	})
	for range 2 {
		if err := l.Open(context.Background(), "opaque-task", target); err != nil {
			t.Fatal(err)
		}
	}
	if paths[0] != paths[1] {
		t.Fatal("repeated opens created unbounded launch files")
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	env, _ := os.ReadFile(filepath.Join(dir, "env"))
	if string(args) != "--remote\n"+target.Endpoint+"\nresume\n"+target.Thread+"\n" || string(env) != workspace+"\n"+target.CodexHome+"\n" {
		t.Fatalf("arguments changed: %q %q", args, env)
	}
	for _, path := range []string{paths[0], filepath.Dir(paths[0])} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatal("launch data permissions", err)
		}
	}
	files, _ := os.ReadDir(l.directory)
	if len(files) != 1 {
		t.Fatal("temporary files leaked")
	}
	script, _ := os.ReadFile(paths[0])
	if strings.Contains(string(script), "opaque-task") {
		t.Fatal("product handle not needed in command")
	}
}

func TestRejectInvalidTargetBeforeOpening(t *testing.T) {
	valid := api.TerminalTarget{Binary: "/bin/codex", Directory: "/tmp", Endpoint: "unix:///tmp/private.sock", Thread: "owned-worker"}
	for _, change := range []func(*api.TerminalTarget){func(v *api.TerminalTarget) { v.Binary = "codex" }, func(v *api.TerminalTarget) { v.Endpoint = "ws://host" }, func(v *api.TerminalTarget) { v.Thread = "--last" }, func(v *api.TerminalTarget) { v.CodexHome = "x\x00" }} {
		v := valid
		change(&v)
		l := New(t.TempDir(), func(context.Context, string) error { t.Fatal("invalid target launched"); return nil })
		if l.Open(context.Background(), "task", v) == nil {
			t.Fatal("invalid target accepted")
		}
	}
}
