//go:build (!darwin || !cgo) && !windows

package desktop

import "github.com/caelis-labs/caelis-bot/internal/taskterminal"

func taskSnapshotStatus() string    { return "unsupported" }
func configureTaskSnapshots() error { return taskterminal.ErrWindowUnsupported }
