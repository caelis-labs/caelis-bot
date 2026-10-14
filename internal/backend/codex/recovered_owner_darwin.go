package codex

import (
	"errors"
	"net"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func socketPeerPID(conn net.Conn) int {
	u, ok := conn.(*net.UnixConn)
	if !ok {
		return 0
	}
	raw, err := u.SyscallConn()
	if err != nil {
		return 0
	}
	pid := 0
	controlErr := raw.Control(func(fd uintptr) {
		pid, err = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	})
	if controlErr != nil || err != nil {
		return 0
	}
	return pid
}

func (o *ownedTools) releaseRecoveredHandles() {}

func stopRecoveredProcess(id ownerProcessIdentity) error {
	if privateOwnerLifecycle(id) != ownerAlive {
		return errors.New("原 owner 身份或状态已改变，未发送退出信号")
	}
	if err := syscall.Kill(id.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if privateOwnerLifecycle(id) != ownerAlive {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	if privateOwnerLifecycle(id) != ownerAlive {
		return nil
	}
	if err := syscall.Kill(id.PID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil // Session.Close still verifies the actual exit before settling.
}
