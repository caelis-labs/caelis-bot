//go:build !darwin || !cgo

package desktop

import "github.com/caelis-labs/caelis-bot/internal/taskterminal"

func taskSnapshotStatus() string    { return "unsupported" }
func configureTaskSnapshots() error { return taskterminal.ErrWindowUnsupported }
