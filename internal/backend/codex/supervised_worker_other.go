//go:build !darwin && !linux

package codex

import (
	"context"
)

func openSupervisedWorkerClient(context.Context, Options, string) (*Client, func(), string, error) {
	return nil, nil, "", ErrOwnedRuntimeUnsupported
}
