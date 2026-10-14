//go:build darwin && !cgo

package codex

import (
	"errors"
	"net"
)

func readSocketPeerIdentity(net.Conn) socketPeerIdentity { return socketPeerIdentity{} }
func (o *ownedTools) releaseRecoveredHandles()           {}
func recoveredToolCleanupUnconfirmed() error {
	return errors.New("原私有 owner 的后台工具清理仍需核对")
}
func replaceRetainedOwnerOnReattach() bool { return true }
func terminateRecoveredNativeOwner(o *recoveredNativeOwner) error {
	return errors.New("原 owner 缺少可绑定身份的退出目标；需在应用外确认并清理原进程")
}
