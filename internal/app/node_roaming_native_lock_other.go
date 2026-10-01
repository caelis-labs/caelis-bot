//go:build !darwin && !linux

package app

import "errors"

func lockNativeRoamingSupervisor(string) (func(), error) {
	return nil, errors.New("native managed lifetime is unavailable on this platform")
}

func lockNativeRoamingIntent(path string) (func(), error) { return lockNativeRoamingSupervisor(path) }
