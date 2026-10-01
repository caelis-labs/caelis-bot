//go:build !darwin && !linux

package codex

import (
	"context"
)

func (s *Session) startSupervised(context.Context, Options) (*Client, error) {
	return nil, ErrOwnedRuntimeUnsupported
}
