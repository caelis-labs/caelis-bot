// Package localipc owns private per-launch IPC endpoints. Payload authentication,
// framing and tool dispatch belong to the caller; OS transport/permissions stay here.
package localipc

import (
	"net"
	"sync"
)

type Transport string

const (
	UnixSocket Transport = "unix"
	NamedPipe  Transport = "named-pipe"
)

type Endpoint struct {
	Transport Transport
	Address   string
}

type Listener struct {
	net.Listener
	endpoint  string
	transport Transport
	cleanup   func() error
	once      sync.Once
	err       error
}

func (l *Listener) Endpoint() string  { return l.endpoint }
func (l *Listener) Address() Endpoint { return Endpoint{Transport: l.transport, Address: l.endpoint} }
func (l *Listener) Close() error {
	l.once.Do(func() { l.err = l.cleanup() })
	return l.err
}
