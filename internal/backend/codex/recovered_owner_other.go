//go:build !darwin && !linux

package codex

import "net"

func socketPeerPID(net.Conn) int                                               { return 0 }
func (c *Client) adoptRecoveredPrivateOwner(string, ownerProcessIdentity) bool { return false }
func privateOwnerLifecycle(ownerProcessIdentity) ownerLifecycle                { return ownerUnknown }
