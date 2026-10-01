//go:build darwin || linux

package nodeagent

import (
	"os"
	"syscall"
	"testing"
)

type enrollmentForeignOwnerFixture struct{ os.FileInfo }

func (enrollmentForeignOwnerFixture) Sys() any { return &syscall.Stat_t{Uid: uint32(os.Geteuid()) + 1} }

func TestCoordinatorIdentityRejectsForeignFileOwner(t *testing.T) {
	if privateFileOwnedByCurrentUser(enrollmentForeignOwnerFixture{}) {
		t.Fatal("coordinator identity accepted a foreign UID")
	}
}
