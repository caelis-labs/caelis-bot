//go:build !darwin && !linux

package caelis

import (
	"context"
	"errors"
)

func startOwnedHost(context.Context, OwnedHostOptions) (*ownedHost, error) {
	return nil, errors.New("owned Caelis native fencing unavailable")
}
func startOwnedHostWithStore(context.Context, OwnedHostOptions, bool) (*ownedHost, error) {
	return nil, errors.New("owned Caelis native fencing unavailable")
}
func (*ownedHost) check(context.Context) error {
	return errors.New("owned Caelis native fencing unavailable")
}
func (*ownedHost) ready(context.Context, string) error {
	return errors.New("owned Caelis native fencing unavailable")
}
func (*ownedHost) stop(context.Context) error {
	return errors.New("owned Caelis native fencing unavailable")
}
