//go:build !darwin && !linux

package workerwire

import (
	"context"
	"errors"
	"io"
)

func DialRelayEndpoint(context.Context, string) (io.ReadWriteCloser, error) {
	return nil, errors.New("private Worker relay unavailable on this platform")
}
