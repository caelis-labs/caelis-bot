//go:build !darwin && !linux

package codex

import (
	"context"
	"errors"
	"time"
)

type OwnedForeground struct{}

func StartOwnedForeground(context.Context, string, string, string, string) (*OwnedForeground, error) {
	return nil, errors.New("owned foreground native fencing unavailable")
}
func (*OwnedForeground) Live() bool { return false }
func (*OwnedForeground) ConfigureDeadline(context.Context, string, time.Time) error {
	return errors.New("owned foreground native fencing unavailable")
}
func (*OwnedForeground) Stop(context.Context) error {
	return errors.New("owned foreground native fencing unavailable")
}
