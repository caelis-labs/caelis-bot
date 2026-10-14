//go:build darwin && cgo

package codex

/*
#cgo LDFLAGS: -lbsm
#include <errno.h>
#include <stdint.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <mach/message.h>
#include <bsm/libbsm.h>
#include <libproc.h>

static int cb_read_peer_token(int fd, uint32_t values[8], int *pid) {
	audit_token_t token;
	socklen_t size = sizeof(token);
	if (getsockopt(fd, SOL_LOCAL, LOCAL_PEERTOKEN, &token, &size) != 0) return errno;
	if (size != sizeof(token)) return EINVAL;
	*pid = audit_token_to_pid(token);
	if (*pid <= 1 || audit_token_to_pidversion(token) <= 0) return EINVAL;
	memcpy(values, token.val, sizeof(token.val));
	return 0;
}

static int cb_signal_peer_token(const uint32_t values[8], int signal) {
	audit_token_t token;
	memcpy(token.val, values, sizeof(token.val));
	return proc_signal_with_audittoken(&token, signal);
}
*/
import "C"

import (
	"errors"
	"net"
	"syscall"
	"time"
	"unsafe"
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
	var identity socketPeerIdentity
	var pid C.int
	var result C.int
	controlErr := raw.Control(func(fd uintptr) {
		result = C.cb_read_peer_token(C.int(fd), (*C.uint32_t)(unsafe.Pointer(&identity.AuditToken[0])), &pid)
	})
	if controlErr != nil || result != 0 {
		return socketPeerIdentity{}
	}
	identity.PID, identity.HasToken = int(pid), true
	return identity
}

func (o *ownedTools) releaseRecoveredHandles() {}

func recoveredToolCleanupUnconfirmed() error {
	return errors.New("原私有 owner 的后台工具清理仍需核对")
}
func replaceRetainedOwnerOnReattach() bool { return true }

// Descendant PIDs discovered after a crash have no stable signal target on
// macOS. Only the exact socket peer's audit token can stop the root safely.
func terminateRecoveredNativeOwner(o *recoveredNativeOwner) error {
	return stopRecoveredProcess(o.identity, o.token, o.hasToken)
}

func stopRecoveredProcess(id ownerProcessIdentity, token [8]uint32, hasToken bool) error {
	if !hasToken {
		return errors.New("原 owner 缺少可绑定身份的退出目标；需在应用外确认并清理原进程")
	}
	if privateOwnerLifecycle(id) != ownerAlive {
		return errors.New("原 owner 身份或状态已改变，未发送退出信号；需在应用外核对")
	}
	signal := func(sig syscall.Signal) error {
		result := C.cb_signal_peer_token((*C.uint32_t)(unsafe.Pointer(&token[0])), C.int(sig))
		if result == 0 {
			return nil
		}
		return errors.Join(errors.New("原 owner 的身份绑定退出信号未被接受；需在应用外核对"), syscall.Errno(result))
	}
	if err := signal(syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if privateOwnerLifecycle(id) != ownerAlive {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := signal(syscall.SIGKILL); err != nil {
		return err
	}
	return nil // Session.Close confirms exit before settlement.
}
