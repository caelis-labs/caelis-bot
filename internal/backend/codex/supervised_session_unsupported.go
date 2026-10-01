//go:build !darwin && !linux

package codex

import (
	"context"
	"errors"
)

func (s *Session) startSupervised(context.Context, Options) (*Client, error) {
	return nil, errors.New("independent owned native watchdog unavailable")
}
