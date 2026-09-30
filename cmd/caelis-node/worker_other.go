//go:build !darwin && !linux

package main

import (
	"context"
	"errors"
	"io"
)

func runWorker(context.Context, []string, io.Writer) error {
	return errors.New("native Worker hosting requires Linux or macOS")
}
