package caelisruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/testsupport/hostfixture"
)

func TestWindowsRuntimeDoesNotInheritPackageDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LOCALAPPDATA", home)
	packageDir := t.TempDir()
	t.Chdir(packageDir)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsRuntimeDirectoryChild$")
	cmd.WaitDelay = time.Second
	if err := prepareCommandDirectory(cmd); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "Caelis Bot", "host-workspace")
	cmd.Env = append(os.Environ(), "CAELIS_DIRECTORY_CHILD="+want)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child CWD: %v %s", err, out)
	}
	if cmd.Dir != want || !localstate.IsPrivate(want) {
		t.Fatal("runtime did not use isolated private workspace")
	}
	if entries, err := os.ReadDir(want); err != nil || len(entries) != 0 {
		t.Fatal("default workspace is not empty", err)
	}
	if got, _ := os.Getwd(); got != packageDir {
		t.Fatal("changed global application CWD")
	}
}

func TestWindowsBackgroundRuntimeAllowsPackageDeletion(t *testing.T) {
	home, packageDir := t.TempDir(), t.TempDir()
	t.Setenv("LOCALAPPDATA", home)
	t.Chdir(packageDir)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsRuntimeDirectoryChild$")
	if err := prepareCommandDirectory(cmd); err != nil {
		t.Fatal(err)
	}
	ready, finish := filepath.Join(home, "ready"), filepath.Join(home, "finish")
	cmd.Env = append(os.Environ(), "CAELIS_DIRECTORY_CHILD="+cmd.Dir, "CAELIS_DIRECTORY_READY="+ready, "CAELIS_DIRECTORY_FINISH="+finish)
	var child *hostfixture.Process
	t.Cleanup(func() {
		if child == nil || child.Exited() {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := child.KillAndWait(cleanup); err != nil {
			t.Error("background child cleanup", err)
		}
	})
	var err error
	child, err = hostfixture.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("background child did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Simulate the GUI releasing its own CWD on exit while its detached service
	// remains alive. Windows must allow deleting the old extraction directory.
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(packageDir); err != nil {
		t.Fatalf("background runtime locked ZIP directory: %v", err)
	}
	if err := os.WriteFile(finish, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsRuntimeDirectoryChild(t *testing.T) {
	want := os.Getenv("CAELIS_DIRECTORY_CHILD")
	if want == "" {
		t.Skip("child only")
	}
	got, err := os.Getwd()
	if err != nil || filepath.Clean(got) != filepath.Clean(want) {
		t.Fatal("inherited removable package directory")
	}
	if ready := os.Getenv("CAELIS_DIRECTORY_READY"); ready != "" {
		if err := os.WriteFile(ready, nil, 0600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(os.Getenv("CAELIS_DIRECTORY_FINISH")); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("parent did not release background child")
	}
}
