//go:build darwin || linux

package workerwire

import (
	"context"
	"io"
	"net"
)

// DialRelayEndpoint opens only an existing same-user private native socket.
func DialRelayEndpoint(ctx context.Context, socket string) (io.ReadWriteCloser, error) {
	if err := privatePath(socket, true); err != nil {
		return nil, err
	}
	return (&net.Dialer{}).DialContext(ctx, "unix", socket)
}
