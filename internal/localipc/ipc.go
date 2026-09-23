// Package localipc owns private per-launch IPC endpoints. Payload authentication,
// framing and tool dispatch belong to the caller; OS transport/permissions stay here.
package localipc

import (
	"net"
	"sync"
)

type Listener struct {
	net.Listener
	endpoint string
	cleanup  func() error
	once     sync.Once
	err      error
}

func (l *Listener) Endpoint() string { return l.endpoint }
func (l *Listener) Close() error {
	l.once.Do(func() { l.err = l.cleanup() })
	return l.err
}
