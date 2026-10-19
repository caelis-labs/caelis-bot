package caelisruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsRuntimeDoesNotInheritPackageDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	packageDir := t.TempDir()
	t.Chdir(packageDir)
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsRuntimeDirectoryChild$")
	if err := prepareCommandDirectory(cmd); err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "CAELIS_DIRECTORY_CHILD="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child CWD: %v %s", err, out)
	}
	if cmd.Dir != home {
		t.Fatal("runtime did not use stable home")
	}
	if got, _ := os.Getwd(); got != packageDir {
		t.Fatal("changed global application CWD")
	}
}

func TestWindowsBackgroundRuntimeAllowsPackageDeletion(t *testing.T) {
	home, packageDir := t.TempDir(), t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Chdir(packageDir)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsRuntimeDirectoryChild$")
	if err := prepareCommandDirectory(cmd); err != nil {
		t.Fatal(err)
	}
	ready, finish := filepath.Join(home, "ready"), filepath.Join(home, "finish")
	cmd.Env = append(os.Environ(), "CAELIS_DIRECTORY_CHILD="+home, "CAELIS_DIRECTORY_READY="+ready, "CAELIS_DIRECTORY_FINISH="+finish)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
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
	if err := cmd.Wait(); err != nil {
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
		for {
			if _, err := os.Stat(os.Getenv("CAELIS_DIRECTORY_FINISH")); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
