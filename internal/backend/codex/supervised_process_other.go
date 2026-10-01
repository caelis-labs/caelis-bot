//go:build !darwin && !linux

package codex

import (
	"context"
	"errors"
	"os"
	"time"
)

type SupervisedRuntimeKind string

const (
	SupervisedCodexStdio       SupervisedRuntimeKind = "codex-stdio"
	SupervisedCodexUnix        SupervisedRuntimeKind = "codex-unix"
	SupervisedCaelisForeground SupervisedRuntimeKind = "caelis-foreground"
)

type SupervisedProcessOptions struct {
	HelperPath, Binary, Directory, Socket, Store string
	Kind                                         SupervisedRuntimeKind
	Stdin, Stdout, Stderr                        *os.File
}
type SupervisedProcess struct{}

func StartSupervisedProcess(context.Context, SupervisedProcessOptions) (*SupervisedProcess, error) {
	return nil, errors.New("independent owned runtime supervision unavailable on this platform")
}
func (*SupervisedProcess) PID() int   { return 0 }
func (*SupervisedProcess) Live() bool { return false }
func (*SupervisedProcess) Renew(context.Context, string, time.Duration) error {
	return errors.New("independent owned runtime supervision unavailable on this platform")
}
func (*SupervisedProcess) Stop(context.Context) error {
	return errors.New("independent owned runtime supervision unavailable on this platform")
}
func RunSupervisedRuntime(context.Context, *os.File) error {
	return errors.New("independent owned runtime supervision unavailable on this platform")
}
