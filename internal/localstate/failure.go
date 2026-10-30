package localstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
)

// Failure projects WriteError for deduplicated diagnostics without paths,
// values, operation IDs or native error prose. It is not another error wrapper.
type Failure struct {
	Kind, Stage, Operation string
	OSCode                 uint64
	ElapsedMS              int64
}

func (f Failure) Reason() string {
	return fmt.Sprintf("kind=%s stage=%s operation=%s os_code=%d", f.Kind, f.Stage, f.Operation, f.OSCode)
}

func DescribeFailure(err error) Failure {
	if err == nil {
		return Failure{}
	}
	f := Failure{Kind: "other"}
	var write *WriteError
	if errors.As(err, &write) {
		f.Stage, f.ElapsedMS = write.Stage, write.Elapsed.Milliseconds()
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		f.Kind = "deadline"
	case errors.Is(err, context.Canceled):
		f.Kind = "cancelled"
	case f.Stage == "encode":
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
