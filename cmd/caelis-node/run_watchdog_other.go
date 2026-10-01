//go:build !darwin && !linux

package main

import (
	"context"
	"errors"
	"io"
)

func runOwnedWatchdog(context.Context, []string, io.Writer) error {
	return errors.New("owned runtime watchdog unavailable on this platform")
}
