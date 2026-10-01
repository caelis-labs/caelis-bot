//go:build (darwin && cgo) || linux

package main

import (
	"testing"
)

func TestManagedForegroundUsesRealNativeProofAndFreshProductJournal(t *testing.T) {
	testManagedForeground(t, false, false)
}
func TestManagedForegroundClosedDisablePublishesAndStopsBeforeRestore(t *testing.T) {
	testManagedForeground(t, true, false)
}
func TestManagedForegroundClosedNativeStartUsesOriginalReceipt(t *testing.T) {
	testManagedForeground(t, true, true)
}
