package codex

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStoppedPrivateOwnerCannotRecoverWithoutVerifiedExit(t *testing.T) {
	for _, life := range []ownerLifecycle{ownerAlive, ownerUnknown} {
		t.Run(map[ownerLifecycle]string{ownerAlive: "still alive", ownerUnknown: "unknown identity"}[life], func(t *testing.T) {
			root := t.TempDir()
			s := NewSession(SessionOptions{Directory: root, StateFile: filepath.Join(root, "binding.json")})
			original := ownerProcessIdentity{PID: 1201, Birth: 42}
			endpoint := "unix://" + filepath.Join(root, "original.sock")
			s.binding.OwnerProcess = &original
			s.binding.OwnerEndpoint = endpoint
			s.ownerLifecycleProbe = func(ownerProcessIdentity) ownerLifecycle { return life }
			s.start = func(context.Context, Options) (*Client, error) {
				t.Fatal("unverified exit started a replacement owner")
				return nil, nil
			}
			if handled, err := s.recoverStoppedPrivateOwner(t.Context()); !handled || err == nil {
				t.Fatal("unverified owner exit accepted", handled, err)
			}
			if s.binding.OwnerEndpoint != endpoint || s.binding.OwnerProcess == nil || *s.binding.OwnerProcess != original || len(s.binding.LostOwners) != 0 {
				t.Fatal("unverified original owner was archived", s.binding)
			}
		})
	}
}
