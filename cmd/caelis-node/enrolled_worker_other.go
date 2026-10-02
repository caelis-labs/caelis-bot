//go:build !darwin && !linux

package main

import (
	"context"
	"errors"
	"io"
)

func connectEnrolledWorker(context.Context, []string, io.Reader, io.Writer) error {
	return errors.New("ordinary enrolled Worker hosting requires Linux or macOS")
}
