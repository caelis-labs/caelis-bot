package codex

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/localipc"
	"golang.org/x/sys/windows"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type windowsOwner struct {
	job      windows.Handle
	identity ownerProcessIdentity
	once     sync.Once
	mu       sync.Mutex
	err      error
}
type windowsSocketConnection struct {
	*socketConnection
	owner *windowsOwner
}

type windowsPipeConnection struct{ in, out net.Conn }

func (c *windowsPipeConnection) Read(b []byte) (int, error)         { return c.out.Read(b) }
func (c *windowsPipeConnection) Write(b []byte) (int, error)        { return c.in.Write(b) }
func (c *windowsPipeConnection) SetWriteDeadline(t time.Time) error { return c.in.SetWriteDeadline(t) }
func (c *windowsPipeConnection) Close() error                       { return errors.Join(c.in.Close(), c.out.Close()) }

// Job membership is established before the server can spawn tools. No PID/name
// discovery is necessary; the native background-terminal cleanup still runs.
func (c *windowsSocketConnection) captureTools()                         {}
func (c *windowsSocketConnection) processIdentity() ownerProcessIdentity { return c.owner.identity }
func (c *windowsSocketConnection) toolCleanupError() error {
	c.owner.mu.Lock()
	defer c.owner.mu.Unlock()
	return c.owner.err
}
func (c *windowsSocketConnection) Close() error { return c.socketConnection.Close() }

// The parent needs overlapped I/O for cancellable deadlines. The child's end is
// a synchronous native handle inherited as stdin/stdout, as App Server expects.
// os.Pipe on Windows uses CreatePipe and does not support SetWriteDeadline.
func windowsStdioPipe(ctx context.Context) (net.Conn, *os.File, error) {
	l, err := localipc.Listen()
	if err != nil {
		return nil, nil, err
	}
	defer l.Close()
	type accepted struct {
		conn net.Conn
		err  error
	}
	ready := make(chan accepted, 1)
	go func() { conn, err := l.Accept(); ready <- accepted{conn, err} }()
	var child *os.File
	for {
		child, err = os.OpenFile(l.Endpoint(), os.O_RDWR, 0)
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err != nil {
		return nil, nil, err
	}
	select {
	case v := <-ready:
		if v.err != nil {
			child.Close()
			return nil, nil, v.err
		}
		fd, ok := v.conn.(interface{ Fd() uintptr })
		var clientPID, serverPID uint32
		if !ok || windows.GetNamedPipeClientProcessId(windows.Handle(fd.Fd()), &clientPID) != nil || windows.GetNamedPipeServerProcessId(windows.Handle(child.Fd()), &serverPID) != nil || clientPID != windows.GetCurrentProcessId() || serverPID != windows.GetCurrentProcessId() {
			v.conn.Close()
			child.Close()
			return nil, nil, errors.New("stdio pipe was not opened by its owner")
		}
		return v.conn, child, nil
	case <-ctx.Done():
		child.Close()
		l.Close()
		v := <-ready
		if v.conn != nil {
			v.conn.Close()
		}
		return nil, nil, ctx.Err()
	}
}

func startProcess(ctx context.Context, opts Options) (connection, func(), error) {
	binary, err := runtimeBinary(opts.Binary)
	if err != nil {
		return nil, nil, err
	}
	endpoint := "stdio://"
	dir := ""
	path := ""
	if opts.Attachable {
		dir, path, err = windowsSocketDirectory(windowsSocketRoots())
		if err != nil {
			return nil, nil, err
		}
		endpoint = "unix://" + filepath.ToSlash(path)
	}
	cmd := exec.Command(binary, appServerArgs(opts, endpoint)...)
	handedOff := false
	defer func() {
		if !handedOff && dir != "" {
			_ = os.RemoveAll(dir)
		}
	}()
	cmd.Dir = opts.Directory
	cmd.Env = ownedEnvironment(cmd.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW}
	var job windows.Handle
	var stdio connection
	var childIn, childOut *os.File
	if !opts.Attachable {
		in, child, e := windowsStdioPipe(ctx)
		if e != nil {
			windows.CloseHandle(job)
			return nil, nil, e
		}
		childIn = child
		out, child, e := windowsStdioPipe(ctx)
		if e != nil {
			in.Close()
			childIn.Close()
			windows.CloseHandle(job)
			return nil, nil, e
		}
		childOut = child
		stdio = &windowsPipeConnection{in, out}
		cmd.Stdin = childIn
		cmd.Stdout = childOut
	}
	if err = ctx.Err(); err == nil {
		err = cmd.Start()
	}
	if childIn != nil {
		childIn.Close()
		childOut.Close()
	}
	if err != nil {
		windows.CloseHandle(job)
		if stdio != nil {
			stdio.Close()
		}
		if dir != "" {
			os.RemoveAll(dir)
		}
		return nil, nil, errors.New("could not start Codex App Server")
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_DUP_HANDLE, false, uint32(cmd.Process.Pid))
	var identity ownerProcessIdentity
	if err == nil {
		identity, err = windowsProcessIdentity(process, cmd.Process.Pid)
		if err == nil {
			job, err = createWindowsOwnerJob(identity)
		}
		if err == nil {
			err = windows.AssignProcessToJobObject(job, process)
		}
		if err == nil {
			// Keep the named Job reopenable across Bot update/crash. This handle
			// lives in the owned, still-suspended server and closes at its exit;
			// it does not grant the server control over any unrelated process.
			var retained windows.Handle
			err = windows.DuplicateHandle(windows.CurrentProcess(), job, process, &retained, 0, false, windows.DUPLICATE_SAME_ACCESS)
		}
		windows.CloseHandle(process)
	}
	if err == nil {
		err = resumeWindowsProcess(uint32(cmd.Process.Pid))
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if stdio != nil {
			stdio.Close()
		}
		windows.CloseHandle(job)
		if dir != "" {
			os.RemoveAll(dir)
		}
		return nil, nil, errors.New("could not establish owned Codex process scope")
	}
	owner := &windowsOwner{job: job, identity: identity}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	stop := func() {
		owner.once.Do(func() {
			owner.mu.Lock()
			owner.err = errors.Join(windows.TerminateJobObject(job, 1), windows.CloseHandle(job))
			owner.mu.Unlock()
			select {
			case <-exited:
			case <-time.After(5 * time.Second):
				owner.mu.Lock()
				owner.err = errors.Join(owner.err, errors.New("owned Codex shutdown not confirmed"))
				owner.mu.Unlock()
				return // Preserve the endpoint for an unconfirmed owner.
			}
			if dir != "" {
				_ = os.RemoveAll(dir)
			}
		})
	}
	if !opts.Attachable {
		handedOff = true
		return stdio, stop, nil
	}
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, e := os.Stat(path); e == nil {
			conn, e := dialExisting(ctx, path)
			if e == nil {
				if conn.peer.PID != identity.PID || privateOwnerLifecycle(identity) != ownerAlive {
					conn.Close()
					stop()
					return nil, nil, errors.New("Codex private endpoint owner mismatch")
				}
				handedOff = true
				return &windowsSocketConnection{conn, owner}, stop, nil
			}
		}
		select {
		case <-ctx.Done():
			stop()
			return nil, nil, ctx.Err()
		case <-exited:
			stop()
			return nil, nil, errors.New("Codex App Server did not expose its private local endpoint")
		case <-ticker.C:
		}
	}
}

// The suspended initial thread cannot spawn children before Job assignment.
func resumeWindowsProcess(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, e := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if e != nil {
			return e
		}
		defer windows.CloseHandle(thread)
		n, _, e := windows.NewLazySystemDLL("kernel32.dll").NewProc("ResumeThread").Call(uintptr(thread))
		if n == 0xffffffff {
			return e
		}
		return nil
	}
	return errors.New("owned initial thread unavailable")
}
