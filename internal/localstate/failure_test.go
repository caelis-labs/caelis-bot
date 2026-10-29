package localstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type privateMarshalFailure struct{}

func (privateMarshalFailure) MarshalJSON() ([]byte, error) {
	return nil, &os.PathError{Op: "private-operation", Path: "private-value", Err: syscall.EACCES}
}

func TestWriteFailureIsSafeAndPreservesCause(t *testing.T) {
	err := Write(filepath.Join(t.TempDir(), "private-path"), privateMarshalFailure{})
	if !errors.Is(err, syscall.EACCES) {
		t.Fatal("lost native cause", err)
	}
	var path *os.PathError
	if !errors.As(err, &path) || path.Path != "private-value" {
		t.Fatal("lost original error for owning diagnostics")
	}
	if strings.Contains(err.Error(), "private-") || strings.Contains(DescribeFailure(err).Reason(), "private-") {
		t.Fatal("private error data leaked")
	}
	if got := DescribeFailure(err); got.Phase != "encode" || got.OSCode != uint64(syscall.EACCES) || got.Operation != "other" {
		t.Fatal("missing safe failure evidence", got)
	}
}

func TestWriteFailureDistinguishesFilesystemFromAdmissionDeadline(t *testing.T) {
	// Observe the native failure itself, separately from the bounded wait.
	if got := DescribeFailure(WriteConfirmed(t.TempDir(), map[string]string{"secret": "private"})); got.Kind != "filesystem" || got.OSCode == 0 {
		t.Fatal("missing filesystem evidence", got)
	}
	path := filepath.Join(t.TempDir(), "blocked.json")
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	writers.Store(path, gate)
	defer writers.Delete(path)
	err := Write(path, map[string]bool{"test": true})
	got := DescribeFailure(err)
	if !errors.Is(err, context.DeadlineExceeded) || got.Kind != "deadline" || got.Phase != "writer_admission" || got.CompletionReady {
		t.Fatal("lost original bounded admission result", err, got)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("admission timeout unexpectedly dispatched a write", err)
	}
}
