//go:build !darwin && !linux

package codex

import (
	"context"
	"time"
)

type OwnedForeground struct{}

func StartOwnedForeground(context.Context, string, string, string, string) (*OwnedForeground, error) {
	return nil, ErrOwnedRuntimeUnsupported
}
func (*OwnedForeground) Live() bool { return false }
func (*OwnedForeground) ConfigureDeadline(context.Context, string, time.Time) error {
	return ErrOwnedRuntimeUnsupported
}
func (*OwnedForeground) Stop(context.Context) error {
	return ErrOwnedRuntimeUnsupported
}
