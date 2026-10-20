//go:build !darwin && !linux

package managedupdate

import "testing"

func TestUnsupportedManagedOwner(t *testing.T) {
	if _, err := acquireLease(t.TempDir()); Code(err) != "platform_unsupported" {
		t.Fatal(err)
	}
	if err := RetainChildLease(t.TempDir()); Code(err) != "platform_unsupported" {
		t.Fatal(err)
	}
}
