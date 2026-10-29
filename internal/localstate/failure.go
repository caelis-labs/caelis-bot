package localstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// Failure contains no paths, values, operation IDs or native error prose.
type Failure struct {
	Kind, Phase, Operation string
	OSCode                 uint64
	ElapsedMS              int64
	CompletionReady        bool
}

func (f Failure) Reason() string {
	return fmt.Sprintf("kind=%s phase=%s operation=%s os_code=%d", f.Kind, f.Phase, f.Operation, f.OSCode)
}

type writeError struct {
	cause           error
	phase           string
	elapsed         time.Duration
	completionReady bool
}

func (e *writeError) Error() string { return "local state save failed: " + DescribeFailure(e).Reason() }
func (e *writeError) Unwrap() error { return e.cause }

func DescribeFailure(err error) Failure {
	if err == nil {
		return Failure{}
	}
	f := Failure{Kind: "other"}
	var write *writeError
	if errors.As(err, &write) {
		f.Phase, f.ElapsedMS, f.CompletionReady = write.phase, write.elapsed.Milliseconds(), write.completionReady
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		f.Kind = "deadline"
	case errors.Is(err, context.Canceled):
		f.Kind = "cancelled"
	case f.Phase == "encode":
		f.Kind = "encoding"
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		f.Kind, f.OSCode = "filesystem", uint64(errno)
	}
	var path *os.PathError
	if errors.As(err, &path) {
		// PathError.Op can also originate in a custom marshaler. Whitelist it.
		switch path.Op {
		case "mkdir", "open", "write", "sync", "close", "rename", "chmod":
			f.Operation = path.Op
		default:
			f.Operation = "other"
		}
	}
	return f
}
