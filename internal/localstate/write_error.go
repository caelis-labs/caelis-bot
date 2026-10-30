package localstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"time"
)

// WriteError preserves the cause without exposing a document path or contents.
// On timeout, Stage is an observation of the still-running writer, not proof
// that it stopped or that the operation had no effect. Elapsed is total wait.
type WriteError struct {
	Stage   string
	Elapsed time.Duration
	Err     error
}

func (e *WriteError) Error() string {
	return fmt.Sprintf("local state write failed (stage=%s elapsed=%s cause=%s)", e.Stage, e.Elapsed.Truncate(time.Millisecond), writeErrorKind(e.Err))
}

func (e *WriteError) Unwrap() error { return e.Err }

func writeErrorKind(err error) string {
	var errno syscall.Errno
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.As(err, &errno):
		return fmt.Sprintf("os_error_%d", errno)
	case errors.Is(err, os.ErrPermission):
		return "permission"
	case errors.Is(err, os.ErrNotExist):
		return "not_exist"
	default:
		return "other"
	}
}

const (
	writeEncode = iota
	writeQueue
	writeDispatch
	writeDirectory
	writeCreate
	writeProtect
	writeContent
	writeSync
	writeClose
	writeRename
	writeParent
)

var writeStages = [...]string{"encode", "queue", "dispatch", "directory", "create", "protect", "write", "sync", "close", "rename", "parent_sync"}

type writeAttempt struct {
	started time.Time
	stage   atomic.Int32
}

func (a *writeAttempt) failure(err error) error {
	if err == nil {
		return nil
	}
	return &WriteError{Stage: writeStages[a.stage.Load()], Elapsed: time.Since(a.started), Err: err}
}

func (a *writeAttempt) run(stage int32, operation func() error) error {
	a.stage.Store(stage)
	return a.failure(operation())
}
