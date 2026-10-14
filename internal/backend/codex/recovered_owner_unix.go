//go:build darwin || linux

package codex

import (
	"errors"
	"sync"
)

// A recovered private socket carries the same lifecycle responsibility as a
// newly launched private owner. A path and handshake alone grant no authority:
// the kernel peer and process birth must match the saved original identity.
func (c *Client) adoptRecoveredPrivateOwner(endpoint string, id ownerProcessIdentity) bool {
	conn, ok := c.rpc.conn.(*socketConnection)
	if !ok || !id.valid() || conn.endpoint != endpoint || conn.peerPID != id.PID || privateOwnerLifecycle(id) != ownerAlive {
		return false
	}
	tools := newOwnedTools(id.PID)
	if tools.failure() != nil || tools.processIdentity() != id {
		tools.releaseRecoveredHandles()
		return false
	}
	owner := &recoveredNativeOwner{endpoint: endpoint, identity: id, tools: tools}
	c.owner, c.shared, c.rpc.stop, c.recoveredPrivate = owner, false, owner.stop, true
	return true
}

type recoveredNativeOwner struct {
	endpoint string
	identity ownerProcessIdentity
	tools    *ownedTools
	mu       sync.Mutex
	stopErr  error
}

func (o *recoveredNativeOwner) terminalEndpoint() string              { return o.endpoint }
func (o *recoveredNativeOwner) processIdentity() ownerProcessIdentity { return o.identity }
func (o *recoveredNativeOwner) captureTools()                         { o.tools.capture() }
func (o *recoveredNativeOwner) toolCleanupError() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return errors.Join(o.tools.failure(), o.stopErr)
}
func (o *recoveredNativeOwner) stop() {
	o.tools.capture()
	o.tools.terminate()
	err := stopRecoveredProcess(o.identity)
	o.mu.Lock()
	o.stopErr = err
	o.mu.Unlock()
}
