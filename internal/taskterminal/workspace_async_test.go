//go:build !windows

package taskterminal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// The terminal accepts the document and stays alive while its shell opens it
// later. Like applicationInstance.Confirm, this owner can confirm an ended
// receipt PID while its application is still present. No native GUI is used.
type asyncTerminalWorkspace struct {
	*documentFixture
	confirms atomic.Int32
}

func (w *asyncTerminalWorkspace) Confirm(ctx context.Context, pid int) error {
	w.confirms.Add(1)
	return w.documentFixture.Confirm(ctx, pid)
}

func TestAsyncTerminalWorkspaceFailureReachesOwningController(t *testing.T) {
	for _, test := range []struct {
		runtime string
		removed bool
	}{{"codex", true}, {"caelis", true}, {"codex", false}, {"caelis", false}} {
		t.Run(test.runtime+map[bool]string{true: "/removed", false: "/retained"}[test.removed], func(t *testing.T) {
			runtime := test.runtime
			root := canonicalTempDir(t)
			workspace := filepath.Join(root, "工作 ' directory")
			if err := os.Mkdir(workspace, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(root, "client-started")
			binary := filepath.Join(root, "client")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\ntouch "+quote(marker)+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			target := api.TerminalTarget{Runtime: runtime, Binary: binary, Directory: workspace, Endpoint: "http://127.0.0.1:1234", Thread: "original-thread", Session: "original-session", Store: root, TokenFile: filepath.Join(root, "token")}
			if runtime == "codex" {
				target.Endpoint = "unix:///tmp/original.sock"
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			w := &asyncTerminalWorkspace{documentFixture: newDocumentFixture()}
			opened, observing, shellDone := make(chan string, 1), make(chan struct{}), make(chan struct{})
			var once sync.Once
			w.read = func() error {
				once.Do(func() {
					close(observing)
					select {
					case <-shellDone:
					case <-ctx.Done():
					}
				})
				return nil
			}
			var mu sync.Mutex
			var last WindowEvent
			m := NewWindowManager(filepath.Join(root, "Terminal"), func(_ context.Context, path string) (Window, error) {
				opened <- path
				return w, nil // Document acceptance precedes shell execution.
			}, func(context.Context, string) (api.TerminalTarget, error) { return target, nil }, func(_ string, e WindowEvent) {
				mu.Lock()
				last = e
				mu.Unlock()
			}, nil)
			defer m.Close()
			result := make(chan error, 1)
			go func() { result <- m.Click(ctx, "original-task") }()
			var path string
			select {
			case path = <-opened:
			case <-ctx.Done():
				t.Fatal("terminal did not accept its document")
			}
			select {
			case <-observing: // The launch callback has returned to the owner.
			case <-ctx.Done():
				t.Fatal("owner did not begin asynchronous observation")
			}
			if test.removed {
				if err := os.Remove(workspace); err != nil {
					t.Fatal(err)
				}
			}
			output, shellErr := exec.CommandContext(ctx, "/bin/sh", path).CombinedOutput()
			close(shellDone)
			launchErr := <-result
			mu.Lock()
			defer mu.Unlock()
			if test.removed {
				if shellErr == nil || string(output) != api.ErrTaskWorkspaceUnavailable.Error()+"\n" {
					t.Fatalf("shell did not fail at cwd: %v %q", shellErr, output)
				}
				if !errors.Is(launchErr, api.ErrTaskWorkspaceUnavailable) {
					t.Fatal("terminal app acceptance hid the asynchronous cd failure", launchErr)
				}
				if last.Phase != WindowUncertain || !errors.Is(last.Err, api.ErrTaskWorkspaceUnavailable) || w.confirms.Load() != 0 {
					t.Fatal("controller confirmed a failed client startup", last, w.confirms.Load())
				}
				if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("client ran in another directory", err)
				}
			} else {
				if shellErr != nil || launchErr != nil || last.Phase != WindowIdle || last.Err != nil || w.confirms.Load() != 1 {
					t.Fatal("valid delayed startup failed", shellErr, launchErr, last, w.confirms.Load())
				}
				if _, err := os.Stat(marker); err != nil {
					t.Fatal("client did not start", err)
				}
			}
		})
	}
}
