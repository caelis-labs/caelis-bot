package hostfixture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

// Process has one waiter, including on cleanup after a canceled test.
type Process struct {
	PID  int
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func Start(cmd *exec.Cmd) (*Process, error) {
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &Process{PID: cmd.Process.Pid, cmd: cmd, done: make(chan struct{})}
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func (p *Process) Done() <-chan struct{} { return p.done }

func (p *Process) Exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *Process) Wait(ctx context.Context) error {
	select {
	case <-p.done:
		return p.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop interrupts an owned child and bounds the time allowed for graceful exit.
func (p *Process) Stop(ctx context.Context) error {
	if p.Exited() {
		return nil
	}
	if runtime.GOOS == "windows" {
		return p.KillAndWait(ctx)
	}
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return errors.Join(err, p.KillAndWait(ctx))
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-p.done:
		if p.err == nil {
			return nil
		}
		var exit *exec.ExitError
		if errors.As(p.err, &exit) {
			if status, ok := exit.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signal() == syscall.SIGINT {
				return nil
			}
		}
		return fmt.Errorf("foreground Host exit: %w", p.err)
	case <-timer.C:
		return errors.Join(fmt.Errorf("foreground Host PID %d did not exit on interrupt", p.PID), p.KillAndWait(ctx))
	case <-ctx.Done():
		return errors.Join(ctx.Err(), p.KillAndWait(ctx))
	}
}

func (p *Process) KillAndWait(ctx context.Context) error {
	if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for PID %d: %w", p.PID, ctx.Err())
	}
}
