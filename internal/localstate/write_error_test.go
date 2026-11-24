package localstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteFailurePreservesCauseAndRedactsPrivatePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private-document")
	if err := os.WriteFile(root, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	err := Write(filepath.Join(root, "child.json"), map[string]string{"secret": "never-in-diagnostics"})
	var failure *WriteError
	var pathError *os.PathError
	if !errors.As(err, &failure) || failure.Stage != "directory" || failure.Elapsed < 0 || !errors.As(err, &pathError) {
		t.Fatalf("missing write cause or timing: %v", err)
	}
	// A completed sub-tick operation may have zero elapsed time on Windows.
	// The held-admission case separately verifies the measured 2-second wait.
	t.Logf("failure stage=%s elapsed_ns=%d path_error=%t", failure.Stage, failure.Elapsed.Nanoseconds(), errors.As(err, &pathError))
	if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), "never-in-diagnostics") {
		t.Fatal("diagnostic disclosed private data")
	}
}

func TestWriteTimeoutKeepsOriginalWriterAdmission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	writers.Store(path, gate)
	defer writers.Delete(path)
	before := time.Now()
	err := Write(path, map[string]string{"state": "new"})
	var failure *WriteError
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &failure) || failure.Stage != "queue" || failure.Elapsed < 2*time.Second || failure.Completion != nil {
		t.Fatalf("missing bounded-wait diagnostic: %v", err)
	}
	if time.Since(before) < 2*time.Second || len(gate) != 1 {
		t.Fatal("timeout released another writer's admission")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("timed-out admission performed a write")
	}
	<-gate
	if err := Write(path, map[string]string{"state": "after-original-completion"}); err != nil {
		t.Fatal(err)
	}
}

func TestWriteTimeoutRetainsActualCommitResult(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "committed"
		if fail {
			name = "failed"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			release := make(chan struct{})
			err := write(path, map[string]int{"value": 1}, 100*time.Millisecond, func(path string, body []byte, a *writeAttempt) error {
				a.stage.Store(writeSync)
				<-release
				if fail {
					return a.run(writeRename, func() error { return os.ErrPermission })
				}
				return writeBytes(path, body, a)
			})
			close(release)
			var timeout *WriteError
			if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &timeout) || timeout.Completion == nil {
				t.Fatalf("in-flight wait lost writer result: %v", err)
			}
			select {
			case result := <-timeout.Completion:
				_, stat := os.Stat(path)
				if result.Elapsed < timeout.Elapsed || fail && (!errors.Is(result.Err, os.ErrPermission) || !errors.Is(stat, os.ErrNotExist)) ||
					!fail && (result.Err != nil || stat != nil) {
					t.Fatalf("wrong original writer outcome: result=%+v stat=%v", result, stat)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("writer completed without an observable result")
			}
		})
	}
}
