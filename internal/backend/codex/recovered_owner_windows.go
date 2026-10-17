package codex

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var kernelOwner = windows.NewLazySystemDLL("kernel32.dll")

func windowsProcessIdentity(h windows.Handle, pid int) (ownerProcessIdentity, error) {
	var born, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &born, &exited, &kernel, &user); err != nil {
		return ownerProcessIdentity{}, err
	}
	return ownerProcessIdentity{PID: pid, Birth: uint64(born.HighDateTime)<<32 | uint64(born.LowDateTime)}, nil
}

func privateOwnerLifecycle(id ownerProcessIdentity) ownerLifecycle {
	if !id.valid() {
		return ownerUnknown
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(id.PID))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return ownerGone
	}
	if err != nil {
		return ownerUnknown
	}
	defer windows.CloseHandle(h)
	actual, err := windowsProcessIdentity(h, id.PID)
	if err != nil {
		return ownerUnknown
	}
	if actual != id {
		return ownerGone
	}
	result, err := windows.WaitForSingleObject(h, 0)
	if err != nil {
		return ownerUnknown
	}
	if result == windows.WAIT_OBJECT_0 {
		return ownerGone
	}
	if result == uint32(windows.WAIT_TIMEOUT) {
		return ownerAlive
	}
	return ownerUnknown
}

func processGone(pid int) bool {
	if pid <= 1 {
		return false
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return true
	}
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	result, err := windows.WaitForSingleObject(h, 0)
	return err == nil && result == windows.WAIT_OBJECT_0
}

func ownerJobName(id ownerProcessIdentity) (*uint16, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return windows.UTF16PtrFromString(fmt.Sprintf(`Local\CaelisBot.Codex.%s.%d.%d`, user.User.Sid.String(), id.PID, id.Birth))
}

// A named Job survives an observer restart while any associated process lives.
// Deliberately omit KILL_ON_JOB_CLOSE: detaching for an update must not kill
// Workers. Explicit Close terminates this exact Job; a future Bot reopens it
// only after verifying the socket peer, process birth and Job membership.
func createWindowsOwnerJob(id ownerProcessIdentity) (windows.Handle, error) {
	name, err := ownerJobName(id)
	if err != nil {
		return 0, err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return 0, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return 0, err
	}
	attr := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	h, _, callErr := kernelOwner.NewProc("CreateJobObjectW").Call(uintptr(unsafe.Pointer(&attr)), uintptr(unsafe.Pointer(name)))
	runtime.KeepAlive(sd)
	if h == 0 {
		return 0, callErr
	}
	if callErr == windows.ERROR_ALREADY_EXISTS {
		windows.CloseHandle(windows.Handle(h))
		return 0, errors.New("Codex owner Job already exists")
	}
	return windows.Handle(h), nil
}

func readSocketPeerIdentity(conn net.Conn) socketPeerIdentity {
	u, ok := conn.(*net.UnixConn)
	if !ok {
		return socketPeerIdentity{}
	}
	raw, err := u.SyscallConn()
	if err != nil {
		return socketPeerIdentity{}
	}
	var pid, returned uint32
	controlErr := raw.Control(func(fd uintptr) {
		// Windows SDK afunix.h: _WSAIOR(IOC_VENDOR, 256).
		err = windows.WSAIoctl(windows.Handle(fd), 0x58000100, nil, 0, (*byte)(unsafe.Pointer(&pid)), 4, &returned, nil, 0)
	})
	// afunix.sys on supported Windows builds fills PID while reporting zero
	// bytes returned. The successful ioctl and initialized PID are authority.
	if controlErr != nil || err != nil || pid <= 1 {
		return socketPeerIdentity{}
	}
	return socketPeerIdentity{PID: int(pid)}
}

func (c *Client) reattachPrivateOwner(old *Client, endpoint string, id ownerProcessIdentity) bool {
	conn, ok := c.rpc.conn.(*socketConnection)
	if !ok || !id.valid() || conn.endpoint != endpoint || conn.peer.PID != id.PID || privateOwnerLifecycle(id) != ownerAlive {
		return false
	}
	if old != nil && old.privateOwnerIdentity() != nil && *old.privateOwnerIdentity() == id {
		c.adoptOwner(old)
		return true
	}
	name, err := ownerJobName(id)
	if err != nil {
		return false
	}
	job, _, _ := kernelOwner.NewProc("OpenJobObjectW").Call(0x0004|0x0008, 0, uintptr(unsafe.Pointer(name))) // QUERY | TERMINATE
	if job == 0 {
		return false
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(id.PID))
	if err != nil {
		windows.CloseHandle(windows.Handle(job))
		return false
	}
	actual, err := windowsProcessIdentity(process, id.PID)
	var member int32
	okNative, _, _ := kernelOwner.NewProc("IsProcessInJob").Call(uintptr(process), job, uintptr(unsafe.Pointer(&member)))
	windows.CloseHandle(process)
	if err != nil || actual != id || okNative == 0 || member == 0 {
		windows.CloseHandle(windows.Handle(job))
		return false
	}
	owner := &windowsOwner{job: windows.Handle(job), identity: id}
	c.owner = &windowsSocketConnection{conn, owner}
	c.rpc.stop = func() {
		owner.once.Do(func() {
			owner.mu.Lock()
			defer owner.mu.Unlock()
			owner.err = errors.Join(windows.TerminateJobObject(owner.job, 1), windows.CloseHandle(owner.job))
		})
	}
	c.recoveredPrivate = true
	return true
}
