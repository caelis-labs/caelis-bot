//go:build !darwin && !linux

package codex

import "net"

func readSocketPeerIdentity(net.Conn) socketPeerIdentity                          { return socketPeerIdentity{} }
func (c *Client) reattachPrivateOwner(*Client, string, ownerProcessIdentity) bool { return false }
func privateOwnerLifecycle(ownerProcessIdentity) ownerLifecycle                   { return ownerUnknown }
