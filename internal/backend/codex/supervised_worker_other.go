//go:build !darwin && !linux

package codex

import (
	"context"
	"errors"
)

func openSupervisedWorkerClient(context.Context, Options, string) (*Client, func(), string, error) {
	return nil, nil, "", errors.New("leased owned Workers unavailable on this platform")
}
