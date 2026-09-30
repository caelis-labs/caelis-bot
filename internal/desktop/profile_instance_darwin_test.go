//go:build darwin && cgo

package desktop

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// A subprocess uses the consumed Wails native lock, without a Bot, runtime,
// windows, credentials, or the daily application's instance identity.
func TestMain(m *testing.M) {
	key := os.Getenv("BOT_PROFILE_INSTANCE_FIXTURE")
	if key == "" {
		os.Exit(m.Run())
	}
	application.New(application.Options{Name: "Disposable profile lock", SingleInstance: &application.SingleInstanceOptions{UniqueID: key}})
	fmt.Println("profile-lock-acquired")
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

func TestNativeProfileInstancesPreserveRecallAndSeparateOwners(t *testing.T) {
	root := t.TempDir()
	appID := "dev.caelis.profilefixture." + filepath.Base(filepath.Dir(root))
	defaultProfile := filepath.Join(root, "default")
	explicitProfile := filepath.Join(root, "explicit")
	for _, path := range []string{defaultProfile, explicitProfile} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(explicitProfile, alias); err != nil {
		t.Fatal(err)
	}
	key := func(path string) string {
		value, err := profileInstanceID(appID, path, defaultProfile)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	t.Cleanup(func() {
		// Only this fixture's synthetic lock files; never the product instance key.
		for _, value := range []string{appID, key(explicitProfile), key(filepath.Join(root, "different"))} {
			_ = os.Remove(filepath.Join(os.TempDir(), value+".lock"))
		}
	})
	command := func(value string) *exec.Cmd {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		t.Cleanup(cancel)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), "BOT_PROFILE_INSTANCE_FIXTURE="+value)
		return cmd
	}
	hold := func(value string) {
		cmd := command(value)
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = input.Close(); _ = cmd.Wait() })
		reader := bufio.NewScanner(output)
		for reader.Scan() {
			if reader.Text() == "profile-lock-acquired" {
				return
			}
		}
		t.Fatal("native fixture owner never acquired lock", reader.Err())
	}
	assertCollision := func(value string) {
		output, err := command(value).CombinedOutput()
		if err != nil || strings.Contains(string(output), "profile-lock-acquired") {
			t.Fatal("same profile opened a second native owner", string(output), err)
		}
	}
	hold(appID) // Default unselected mode retains the original native key.
	assertCollision(key(defaultProfile))
	hold(key(explicitProfile))
	assertCollision(key(alias))
	output, err := command(key(filepath.Join(root, "different"))).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "profile-lock-acquired") {
		t.Fatal("different explicit profiles collided", string(output), err)
	}
}
