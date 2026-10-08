//go:build !windows

package taskterminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestLaunchPreservesArgumentsAndCleansConfirmedAttempts(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "work ' $(touch unexpected)")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "codex ' $(touch unexpected)")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE_ARGS\"\nprintf '%s\\n' \"$PWD\" \"$CODEX_HOME\" > \"$CAPTURE_ENV\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	target := api.TerminalTarget{Runtime: "codex", Binary: binary, Endpoint: "unix:///tmp/runtime ' $(touch unexpected).sock", Thread: "owned-worker-1", Directory: workspace, CodexHome: filepath.Join(dir, "home ' $(touch unexpected)")}
	var paths []string
	l := New(filepath.Join(dir, "launch"), func(ctx context.Context, path string) error {
		paths = append(paths, path)
		for _, p := range []string{path, filepath.Dir(path)} {
			info, err := os.Stat(p)
			if err != nil || info.Mode().Perm() != 0700 {
				t.Error("nonprivate launch file", err)
			}
		}
		cmd := exec.CommandContext(ctx, "/bin/sh", path)
		cmd.Env = append(os.Environ(), "CAPTURE_ARGS="+filepath.Join(dir, "args"), "CAPTURE_ENV="+filepath.Join(dir, "env"))
		return cmd.Run()
	})
	for range 2 {
		if err := l.Open(context.Background(), "opaque-task", target); err != nil {
			t.Fatal(err)
		}
	}
	if paths[0] == paths[1] {
		t.Fatal("different attempts shared a receipt target")
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	env, _ := os.ReadFile(filepath.Join(dir, "env"))
	if string(args) != "-c\ncheck_for_update_on_startup=false\n--remote\n"+target.Endpoint+"\nresume\n"+target.Thread+"\n" || string(env) != workspace+"\n"+target.CodexHome+"\n" {
		t.Fatalf("arguments changed: %q %q", args, env)
	}
	files, _ := os.ReadDir(l.directory)
	if len(files) != 0 {
		t.Fatal("temporary files leaked")
	}
}

func TestTerminalWaitsForExecutionAndRevokesLateConsent(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	binary := filepath.Join(dir, "fake-codex")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf done > "+quote(marker)+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	target := api.TerminalTarget{Runtime: "codex", Binary: binary, Directory: dir, Endpoint: "unix:///tmp/fixture.sock", Thread: "owned"}
	for _, consent := range []bool{true, false} {
		t.Run(fmt.Sprint(consent), func(t *testing.T) {
			_ = os.Remove(marker)
			opened := make(chan string, 1)
			done := make(chan error, 1)
			l := New(filepath.Join(dir, "launch"), func(_ context.Context, path string) error { opened <- path; return nil })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			go func() { done <- l.Open(ctx, "owned", target) }()
			path := <-opened
			script, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				t.Fatal("open receipt confused with execution", err)
			case <-time.After(25 * time.Millisecond):
			}
			if consent {
				if out, err := exec.Command("/bin/sh", path).CombinedOutput(); err != nil {
					t.Fatal(string(out), err)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(marker); err != nil {
					t.Fatal("runtime not executed", err)
				}
			} else {
				cancel()
				if err := <-done; !errors.Is(err, ErrUnconfirmed) {
					t.Fatal(err)
				}
				// The native request may have been accepted before cancellation.
				// Its late shell must still find the command and safely reject it.
				if _, err := os.Stat(path); err != nil {
					t.Fatal("revoked command path disappeared before shell read", err)
				}
				if out, err := exec.Command("/bin/sh", path).CombinedOutput(); err == nil || !strings.Contains(string(out), "expired") {
					t.Fatal("late shell did not reject revoked command", string(out), err)
				}
				// Even a terminal that buffered the original script cannot execute a
				// revoked attempt after its user eventually accepts the dialog.
				cmd := exec.Command("/bin/sh")
				cmd.Stdin = strings.NewReader(string(script))
				out, err := cmd.CombinedOutput()
				if err == nil || !strings.Contains(string(out), "expired") {
					t.Fatal(string(out), err)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("late consent launched runtime")
				}
			}
			files, _ := os.ReadDir(l.directory)
			want := 0
			if !consent {
				want = 1 // Retained until the owning terminal is proven closed.
			}
			if len(files) != want {
				t.Fatal("unexpected attempt directories", len(files), want)
			}
		})
	}
}

func TestProvenUnsubmittedLaunchCleansScriptImmediately(t *testing.T) {
	root := t.TempDir()
	var path string
	l := New(filepath.Join(root, "Terminal"), func(_ context.Context, command string) error {
		path = command
		return NotLaunched(errors.New("synthetic pre-dispatch failure"))
	})
	target := api.TerminalTarget{Runtime: "setup", Binary: "/usr/bin/true", Directory: root}
	if err := l.Open(t.Context(), "owned", target); !errors.Is(err, ErrLaunchNotSubmitted) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("proven unsubmitted script remained", err)
	}
}

func TestRejectInvalidTargetBeforeOpening(t *testing.T) {
	valid := api.TerminalTarget{Runtime: "codex", Binary: "/bin/codex", Directory: "/tmp", Endpoint: "unix:///tmp/private.sock", Thread: "owned-worker"}
	for _, change := range []func(*api.TerminalTarget){func(v *api.TerminalTarget) { v.Binary = "codex" }, func(v *api.TerminalTarget) { v.Endpoint = "ws://host" }, func(v *api.TerminalTarget) { v.Thread = "--last" }, func(v *api.TerminalTarget) { v.CodexHome = "x\x00" }} {
		v := valid
		change(&v)
		l := New(t.TempDir(), func(context.Context, string) error { t.Fatal("invalid target launched"); return nil })
		if l.Open(context.Background(), "task", v) == nil {
			t.Fatal("invalid target accepted")
		}
	}
}

func TestCaelisAttachUsesExistingSessionAndCredentialPath(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "caelis ' $(touch bad)")
	capture := filepath.Join(dir, "args")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE_ARGS\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	target := api.TerminalTarget{Runtime: "caelis", Binary: binary, Directory: dir, Endpoint: "http://127.0.0.1:9876", Session: "worker-123", Store: filepath.Join(dir, "store ' special"), TokenFile: filepath.Join(dir, "host ' token")}
	launcher := New(filepath.Join(dir, "launch"), func(ctx context.Context, path string) error {
		cmd := exec.CommandContext(ctx, "/bin/sh", path)
		cmd.Env = append(os.Environ(), "CAPTURE_ARGS="+capture)
		return cmd.Run()
	})
	if err := launcher.Open(t.Context(), "opaque", target); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(capture)
	want := "attach\n--control-url\n" + target.Endpoint + "\n--session\n" + target.Session + "\n--store-dir\n" + target.Store + "\n--control-token-file\n" + target.TokenFile + "\n"
	if string(args) != want {
		t.Fatalf("attach arguments changed: %q", args)
	}
	for _, change := range []func(*api.TerminalTarget){func(v *api.TerminalTarget) { v.Endpoint = "http://remote.example" }, func(v *api.TerminalTarget) { v.Session = "--new" }, func(v *api.TerminalTarget) { v.TokenFile = "relative" }, func(v *api.TerminalTarget) { v.Runtime = "other" }} {
		v := target
		change(&v)
		if _, err := Script(v); err == nil {
			t.Fatal("invalid target accepted", v)
		}
	}
}
