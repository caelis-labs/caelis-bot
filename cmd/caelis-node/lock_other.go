//go:build !darwin && !linux

package main

import "errors"

func lockProfile(string) (func(), error) {
	return nil, errors.New("headless profile ownership unavailable on this platform")
}
