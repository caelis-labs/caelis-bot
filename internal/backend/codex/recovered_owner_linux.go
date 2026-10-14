package codex

import (
	"errors"
	"net"
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
		var cred *unix.Ucred
		cred, err = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err == nil {
			pid = int(cred.Pid)
		}
	})
	if controlErr != nil || err != nil {
		return 0
	}
	return pid
}

func (o *ownedTools) releaseRecoveredHandles() { o.releaseHandles() }

func stopRecoveredProcess(id ownerProcessIdentity) error {
	fd, err := unix.PidfdOpen(id.PID, 0)
	if err != nil {
		return errors.New("原 owner 稳定进程句柄不可用，未发送退出信号")
	}
	defer unix.Close(fd)
	if privateOwnerLifecycle(id) != ownerAlive {
		return errors.New("原 owner 身份或状态已改变，未发送退出信号")
	}
	if err := signalLinuxHandle(fd, id.PID, id.Birth, unix.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if privateOwnerLifecycle(id) != ownerAlive {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := signalLinuxHandle(fd, id.PID, id.Birth, unix.SIGKILL); err != nil {
		return err
	}
	return nil // Session.Close still verifies the actual exit before settling.
}
