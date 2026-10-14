//go:build darwin || linux

package codex

import (
	"errors"
	"sync"
)

// Every private reattach, including an in-process retained owner, uses this
// same peer check before any original thread or receipt is read.
func (c *Client) matchesPrivateOwner(endpoint string, id ownerProcessIdentity) bool {
	conn, ok := c.rpc.conn.(*socketConnection)
	return ok && id.valid() && conn.endpoint == endpoint && conn.peer.PID == id.PID && privateOwnerLifecycle(id) == ownerAlive
}

func (c *Client) reattachPrivateOwner(old *Client, endpoint string, id ownerProcessIdentity) bool {
	if !c.matchesPrivateOwner(endpoint, id) {
		return false
	}
	if old != nil && !replaceRetainedOwnerOnReattach() {
		c.adoptOwner(old)
		return true
	}
	conn := c.rpc.conn.(*socketConnection)
	tools := newOwnedTools(id.PID)
	if tools.failure() != nil || tools.processIdentity() != id {
		tools.releaseRecoveredHandles()
		return false
	}
	owner := &recoveredNativeOwner{endpoint: endpoint, identity: id, tools: tools, token: conn.peer.AuditToken, hasToken: conn.peer.HasToken}
	c.owner, c.shared, c.rpc.stop, c.recoveredPrivate = owner, false, owner.stop, true
	return true
}

type recoveredNativeOwner struct {
	endpoint string
	identity ownerProcessIdentity
	tools    *ownedTools
	token    [8]uint32
	hasToken bool
	mu       sync.Mutex
	stopErr  error
}

func (o *recoveredNativeOwner) terminalEndpoint() string              { return o.endpoint }
func (o *recoveredNativeOwner) processIdentity() ownerProcessIdentity { return o.identity }
func (o *recoveredNativeOwner) captureTools()                         { o.tools.capture() }
func (o *recoveredNativeOwner) toolCleanupError() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return errors.Join(o.tools.failure(), o.stopErr, recoveredToolCleanupUnconfirmed())
}
func (o *recoveredNativeOwner) stop() {
	o.tools.capture()
	err := terminateRecoveredNativeOwner(o)
	o.mu.Lock()
	o.stopErr = err
	o.mu.Unlock()
}
