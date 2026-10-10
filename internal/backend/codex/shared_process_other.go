//go:build !darwin && !linux

package codex

func processGone(int) bool { return false }
