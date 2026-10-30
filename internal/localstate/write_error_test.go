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
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &failure) || failure.Stage != "queue" || failure.Elapsed < 2*time.Second {
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
