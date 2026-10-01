//go:build (darwin && cgo) || linux

package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagedSessionWatchdogHelper(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) && os.Args[i+1] == "owned-runtime-watchdog" {
			if err := RunSupervisedRuntime(context.Background(), os.NewFile(3, "owned-watchdog")); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			os.Exit(0)
		}
	}
}
func managedSessionHelper(t *testing.T) string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	helper := filepath.Join(t.TempDir(), "watchdog")
	body := "#!/bin/sh\nexec " + quote(binary) + " -test.run='^TestManagedSessionWatchdogHelper$' -- \"$@\" 2>" + quote(helper+".log") + "\n"
	if err = os.WriteFile(helper, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return helper
}
func TestManagedSessionLaunchesUnderIndependentLeaseDeadline(t *testing.T) {
	binary, pid := fixtureBinary(t, "owned-tool")
	helper := managedSessionHelper(t)
	s := NewSession(SessionOptions{ForceOwned: true, WatchdogHelper: helper, Binary: binary, Directory: t.TempDir()})
	if err := s.OwnedRuntimeReady(t.Context()); err == nil {
		t.Fatal("unproven helper filename supplied runtime eligibility")
	}
	if err := s.VerifyOwnedSupervisor(t.Context()); err != nil {
		t.Fatal(err)
	}

	if _, err := s.startSupervised(t.Context(), Options{Binary: binary, Directory: t.TempDir()}); err == nil {
		t.Fatal("native started without independent lease deadline")
	}
	if err := s.ConfigureOwnedDeadline(t.Context(), "owned-epoch", time.Now().Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	client, err := s.startSupervised(t.Context(), Options{Binary: binary, Directory: t.TempDir()})
	if err != nil {
		b, _ := os.ReadFile(helper + ".log")
		t.Log(string(b))
		t.Fatal(err)
	}
	s.client = client
	if err = s.OwnedRuntimeReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = s.ConfigureOwnedDeadline(t.Context(), "another-epoch", time.Now().Add(5*time.Second)); err == nil {
		t.Fatal("old native generation accepted another lease epoch")
	}
	if err = s.FenceStop(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertReaped(t, pid)
	if err = s.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisedAuthPreflightCreatesNoNativeTurn(t *testing.T) {
	binary, pid := fixtureBinary(t, "owned-tool")
	if _, err := ProbeSupervisedAuth(t.Context(), managedSessionHelper(t), binary, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	assertReaped(t, pid)
}
