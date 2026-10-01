//go:build !darwin && !linux

package caelis

func ProbeOwnedStore(string, string) (bool, string) { return false, "unsupported-platform" }
