package codex

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type pipeConnection struct {
	in, out *os.File
	tools   *ownedTools
}

func (p *pipeConnection) Read(b []byte) (int, error)         { return p.out.Read(b) }
func (p *pipeConnection) Write(b []byte) (int, error)        { return p.in.Write(b) }
func (p *pipeConnection) SetWriteDeadline(t time.Time) error { return p.in.SetWriteDeadline(t) }
func (p *pipeConnection) captureTools()                      { p.tools.capture() }
func (p *pipeConnection) toolCleanupError() error            { return p.tools.failure() }
func (p *pipeConnection) Close() error {
	p.tools.capture()
	return errors.Join(p.in.Close(), p.out.Close())
}

func startProcess(ctx context.Context, opts Options) (connection, func(), error) {
	path, err := runtimeBinary(opts.Binary)
	if err != nil {
		return nil, nil, err
	}
	// CLI release numbers are not App Server protocol versions. Compatibility
	// is established on the wire, using the same handshake as shared endpoints.
	cmd := exec.Command(path, "app-server", "--listen", "stdio://")
	cmd.Dir = opts.Directory
	cmd.Env = ownedEnvironment(cmd.Environ()) // Environ also sets PWD to the actual working directory.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		inRead.Close()
		inWrite.Close()
		return nil, nil, err
	}
	// Use files, not exec's copy goroutines or StdoutPipe: Wait must not close
	// stdout before the protocol reader consumes a final response then EOF.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		inRead.Close()
		inWrite.Close()
		outRead.Close()
		outWrite.Close()
		return nil, nil, err
	}
	cmd.Stdin = inRead
	cmd.Stdout = outWrite
	cmd.Stderr = devNull
	err = ctx.Err()
	if err == nil {
		err = cmd.Start()
	}
	inRead.Close()
	outWrite.Close()
	devNull.Close()
	if err != nil {
		inWrite.Close()
		outRead.Close()
		return nil, nil, errors.New("could not start Codex App Server")
	}
	owned := newOwnedTools(cmd.Process.Pid)
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	stop := func() {
		owned.terminate()
		// os.Process coordinates Signal/Kill with Wait, preventing a recycled PID
		// from being signalled after reaping. This owns only the direct server;
		// Session first interrupts and requests native background-terminal cleanup.
		// Escaped descendants cannot be inferred or globally killed by name.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-exited:
			return
		case <-timer.C:
		}
		_ = cmd.Process.Kill()
		<-exited
	}
	return &pipeConnection{inWrite, outRead, owned}, stop, nil
}
