//go:build !darwin

package codex

import (
	"context"
	"errors"
)

func startProcess(context.Context, Options) (connection, func(), error) {
	return nil, nil, errors.New("native Codex process ownership is implemented for macOS only")
}
