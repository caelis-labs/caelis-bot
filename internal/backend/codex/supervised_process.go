//go:build darwin || linux

package codex

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type SupervisedRuntimeKind string

const (
	SupervisedCodexStdio       SupervisedRuntimeKind = "codex-stdio"
	SupervisedCodexUnix        SupervisedRuntimeKind = "codex-unix"
	SupervisedCaelisForeground SupervisedRuntimeKind = "caelis-foreground"
)

// SupervisedProcessOptions is assembled by the native owner, outside node RPC,
// renderer and model contracts. The helper accepts only these runtime purposes.
type SupervisedProcessOptions struct {
	HelperPath, Binary, Directory, Socket, Store string
	Kind                                         SupervisedRuntimeKind
	Stdin, Stdout, Stderr                        *os.File
}
type supervisorFrame struct {
	ID                                      uint64
	Method                                  string
	Kind                                    SupervisedRuntimeKind `json:",omitempty"`
	Binary, Directory, Socket, Store, Epoch string                `json:",omitempty"`
	DeadlineNs                              int64                 `json:",omitempty"`
	PID                                     int                   `json:",omitempty"`
	Stopped                                 bool                  `json:",omitempty"`
	Fault                                   string                `json:",omitempty"`
}

func writeSupervisor(w io.Writer, f supervisorFrame) error {
	b, err := json.Marshal(f)
	if err != nil || len(b) > 16384 {
		return errors.New("owned watchdog frame limit")
	}
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], uint32(len(b)))
	for _, p := range [][]byte{h[:], b} {
		for len(p) > 0 {
			n, err := w.Write(p)
			if err != nil {
				return err
			}
			if n <= 0 {
				return io.ErrShortWrite
			}
			p = p[n:]
		}
	}
	return nil
}
func readSupervisor(r io.Reader) (supervisorFrame, error) {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return supervisorFrame{}, err
	}
	n := binary.BigEndian.Uint32(h[:])
	if n == 0 || n > 16384 {
		return supervisorFrame{}, errors.New("owned watchdog frame limit")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return supervisorFrame{}, err
	}
	var f supervisorFrame
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&f) != nil || decoder.Decode(new(any)) != io.EOF || f.ID == 0 {
		return f, errors.New("owned watchdog invalid frame")
	}
	return f, nil
}

type SupervisedProcess struct {
	conn    net.Conn
	cmd     *exec.Cmd
	pid     int
	gate    chan struct{}
	exited  chan struct{}
	next    atomic.Uint64
	mu      sync.Mutex
	stopped bool
	stopErr error
}

func StartSupervisedProcess(ctx context.Context, o SupervisedProcessOptions) (*SupervisedProcess, error) {
	if !filepath.IsAbs(o.HelperPath) || !filepath.IsAbs(o.Binary) || !filepath.IsAbs(o.Directory) {
		return nil, errors.New("owned watchdog requires explicit native executable paths")
	}
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(sockets[0])
	unix.CloseOnExec(sockets[1])
	a := os.NewFile(uintptr(sockets[0]), "watchdog-owner")
	b := os.NewFile(uintptr(sockets[1]), "watchdog-child")
	defer b.Close()
	conn, err := net.FileConn(a)
	a.Close()
	if err != nil {
		return nil, err
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		conn.Close()
		return nil, err
	}
	defer null.Close()
	files := []*os.File{b, o.Stdin, o.Stdout, o.Stderr}
	for i := 1; i < len(files); i++ {
		if files[i] == nil {
			files[i] = null
		}
	}
	cmd := exec.Command(o.HelperPath, "owned-runtime-watchdog")
	cmd.ExtraFiles = files
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.Env = ownedEnvironment(os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err = ctx.Err(); err == nil {
		err = cmd.Start()
	}
	if err != nil {
		conn.Close()
		return nil, errors.New("owned watchdog did not start")
	}
	p := &SupervisedProcess{conn: conn, cmd: cmd, gate: make(chan struct{}, 1), exited: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.exited); _ = conn.Close() }()
	reply, err := p.call(ctx, supervisorFrame{Method: "start", Kind: o.Kind, Binary: o.Binary, Directory: o.Directory, Socket: o.Socket, Store: o.Store})
	if err != nil || reply.PID <= 1 {
		_ = conn.Close()
		return nil, errors.New("owned watchdog runtime launch unavailable")
	}
	p.pid = reply.PID
	return p, nil
}
func (p *SupervisedProcess) PID() int { return p.pid }
func (p *SupervisedProcess) Live() bool {
	select {
	case <-p.exited:
		return false
	default:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.stopped
}
func (p *SupervisedProcess) call(ctx context.Context, f supervisorFrame) (supervisorFrame, error) {
	select {
	case p.gate <- struct{}{}:
	case <-ctx.Done():
		return supervisorFrame{}, ctx.Err()
	case <-p.exited:
		return supervisorFrame{}, errors.New("owned watchdog stopped")
	}
	defer func() { <-p.gate }()
	f.ID = p.next.Add(1)
	stop := context.AfterFunc(ctx, func() { _ = p.conn.Close() })
	defer stop()
	if err := writeSupervisor(p.conn, f); err != nil {
		return supervisorFrame{}, err
	}
	reply, err := readSupervisor(p.conn)
	if err != nil || reply.ID != f.ID || reply.Fault != "" {
		return supervisorFrame{}, errors.New("owned watchdog fence unavailable")
	}
	return reply, nil
}

// Renew receives the already conservative native owner deadline. Empty epoch
// is preparation keepalive only; the first installed epoch fixes this generation.
func (p *SupervisedProcess) Renew(ctx context.Context, epoch string, remaining time.Duration) error {
	if remaining <= time.Second || remaining > 45*time.Second {
		return errors.New("owned watchdog requires remaining conservative lease lifetime")
	}
	clock, err := supervisorClock()
	if err != nil {
		return err
	}
	_, err = p.call(ctx, supervisorFrame{Method: "renew", Epoch: epoch, DeadlineNs: clock + remaining.Nanoseconds() - int64(time.Second)})
	if err != nil {
		_ = p.conn.Close()
	}
	return err
}
func (p *SupervisedProcess) Stop(ctx context.Context) error {
	p.mu.Lock()
	done := p.stopped
	prior := p.stopErr
	p.mu.Unlock()
	if done {
		return prior
	}
	reply, err := p.call(ctx, supervisorFrame{Method: "stop"})
	if err == nil && !reply.Stopped {
		err = errors.New("owned watchdog stop unconfirmed")
	}
	_ = p.conn.Close()
	p.mu.Lock()
	p.stopped = true
	p.stopErr = err
	p.mu.Unlock()
	return err
}

// RunSupervisedRuntime owns one foreground child, its exact stable process
// handles and the descendant captures. Descriptor EOF or deadline independently
// fences the native process even when the application owner is forcibly killed.
// No PIDs, shell body, network listeners or auth/account operations are accepted.
func RunSupervisedRuntime(ctx context.Context, control *os.File) error {
	conn, err := net.FileConn(control)
	control.Close()
	if err != nil {
		return err
	}
	defer conn.Close()
	first, err := readSupervisor(conn)
	if err != nil || first.Method != "start" || !filepath.IsAbs(first.Binary) || !filepath.IsAbs(first.Directory) {
		return errors.New("owned watchdog startup invalid")
	}
	var args []string
	switch first.Kind {
	case SupervisedCodexStdio:
		if first.Socket != "" || first.Store != "" {
			return errors.New("owned watchdog purpose mismatch")
		}
		args = []string{"app-server"}
	case SupervisedCodexUnix:
		if !filepath.IsAbs(first.Socket) || first.Store != "" {
			return errors.New("owned watchdog private endpoint required")
		}
		args = []string{"app-server", "--listen", "unix://" + first.Socket}
	case SupervisedCaelisForeground:
		if !filepath.IsAbs(first.Store) || first.Socket != "" {
			return errors.New("owned watchdog target Store required")
		}
		args = []string{"serve", "--store-dir", first.Store, "--listen", "127.0.0.1:0"}
	default:
		return errors.New("owned watchdog runtime purpose unsupported")
	}
	cmd := exec.Command(first.Binary, args...)
	cmd.Dir = first.Directory
	cmd.Env = ownedEnvironment(os.Environ())
	cmd.Stdin = os.NewFile(4, "native-input")
	cmd.Stdout = os.NewFile(5, "native-output")
	cmd.Stderr = os.NewFile(6, "native-error")
	var nativeInput io.WriteCloser
	var nativeOutput io.ReadCloser
	if first.Kind == SupervisedCodexStdio {
		cmd.Stdin = nil
		cmd.Stdout = nil
		nativeInput, err = cmd.StdinPipe()
		if err != nil {
			return err
		}
		nativeOutput, err = cmd.StdoutPipe()
		if err != nil {
			return err
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if first.Kind == SupervisedCaelisForeground {
		home := filepath.Join(first.Store, ".native-home")
		cmd.Env = append(cmd.Env, "HOME="+home, "XDG_CONFIG_HOME="+home)
	}
	if err = cmd.Start(); err != nil {
		return errors.New("owned watchdog native process unavailable")
	}
	tools := newOwnedTools(cmd.Process.Pid)
	tools.capture()
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	stopNative := func() error {
		freezeErr := (&pipeConnection{tools: tools}).freezeOwned()
		childrenErr := tools.killFencedChildren()
		killErr := cmd.Process.Kill()
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}
		select {
		case <-exited:
		case <-time.After(time.Second):
			killErr = errors.Join(killErr, errors.New("owned watchdog native exit unconfirmed"))
		}
		return errors.Join(freezeErr, childrenErr, killErr, tools.failure())
	}
	defer stopNative()
	if err = tools.failure(); err != nil {
		return err
	}
	if err = writeSupervisor(conn, supervisorFrame{ID: first.ID, PID: cmd.Process.Pid}); err != nil {
		return err
	}
	incoming := make(chan supervisorFrame, 1)
	broken := make(chan struct{})
	var brokenOnce sync.Once
	breakPipe := func() { brokenOnce.Do(func() { close(broken) }) }
	if nativeInput != nil {
		defer nativeInput.Close()
		defer nativeOutput.Close()
		go func() { _, _ = io.Copy(nativeInput, os.NewFile(4, "owner-native-input")); breakPipe() }()
		go func() { _, _ = io.Copy(os.NewFile(5, "owner-native-output"), nativeOutput); breakPipe() }()
	}
	go func() {
		defer breakPipe()
		for {
			f, err := readSupervisor(conn)
			if err != nil {
				return
			}
			select {
			case incoming <- f:
			case <-ctx.Done():
				return
			}
		}
	}()
	clock, err := supervisorClock()
	if err != nil {
		return stopNative()
	}
	deadline := clock + int64(45*time.Second)
	epoch := ""
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	last := first.ID
	for {
		select {
		case <-ctx.Done():
			return stopNative()
		case <-broken:
			return stopNative()
		case <-exited:
			return stopNative()
		case <-tick.C:
			now, clockErr := supervisorClock()
			if clockErr != nil {
				return stopNative()
			}
			if now >= deadline {
				return stopNative()
			}
			tools.capture()
			if err = tools.failure(); err != nil {
				return stopNative()
			}
		case f := <-incoming:
			if f.ID <= last || f.Kind != "" || f.Binary != "" || f.Directory != "" || f.Socket != "" || f.Store != "" || f.PID != 0 || f.Stopped || f.Fault != "" {
				return stopNative()
			}
			last = f.ID
			if f.Method == "stop" {
				err = stopNative()
				reply := supervisorFrame{ID: f.ID, Stopped: err == nil}
				if err != nil {
					reply.Fault = "native-stop-unconfirmed"
				}
				_ = writeSupervisor(conn, reply)
				return err
			}
			now, clockErr := supervisorClock()
			if clockErr != nil {
				return stopNative()
			}
			if f.Method != "renew" || f.DeadlineNs <= now || f.DeadlineNs-now > int64(45*time.Second) || (epoch != "" && f.Epoch != epoch) {
				return stopNative()
			}
			if epoch == "" {
				epoch = f.Epoch
			}
			deadline = f.DeadlineNs
			tools.capture()
			if tools.failure() != nil {
				return stopNative()
			}
			if err = writeSupervisor(conn, supervisorFrame{ID: f.ID, PID: cmd.Process.Pid}); err != nil {
				return stopNative()
			}
		}
	}
}

func supervisorClock() (int64, error) {
	var ts unix.Timespec
	err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts)
	return ts.Nano(), err
}
