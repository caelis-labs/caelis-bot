//go:build !darwin && !linux && !windows

package codex

func processGone(int) bool { return false }
