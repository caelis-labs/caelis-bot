package codex

import (
	"errors"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

func readSocketPeerIdentity(conn net.Conn) socketPeerIdentity {
	u, ok := conn.(*net.UnixConn)
	if !ok {
		return socketPeerIdentity{}
	}
	raw, err := u.SyscallConn()
	if err != nil {
		return socketPeerIdentity{}
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
		return socketPeerIdentity{}
	}
	return socketPeerIdentity{PID: pid}
}

func (o *ownedTools) releaseRecoveredHandles() { o.releaseHandles() }

func recoveredToolCleanupUnconfirmed() error { return nil }
func replaceRetainedOwnerOnReattach() bool   { return false }

func terminateRecoveredNativeOwner(o *recoveredNativeOwner) error {
	o.tools.terminate()
	return stopRecoveredProcess(o.identity, o.token, o.hasToken)
}

func stopRecoveredProcess(id ownerProcessIdentity, _ [8]uint32, _ bool) error {
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
