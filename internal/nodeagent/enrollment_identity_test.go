package nodeagent

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func TestClosedCoordinatorVerificationUsesActualEnrolledIdentityWithoutWrites(t *testing.T) {
	directory := t.TempDir()
	service, err := New(Options{Directory: directory, NodeID: "actual-native-machine", Join: api.NodeLocal})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := service.NativeEnrollmentIdentity()
	if err != nil || identity.NodeID != "actual-native-machine" || identity.Directory != directory {
		t.Fatal("presentation local alias replaced native enrollment", identity, err)
	}
	path := filepath.Join(directory, "node.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(t.Context(), []string{"verify-join-directory", "--directory", directory, "--node-id", identity.NodeID}, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := Run(t.Context(), []string{"verify-join-directory", "--directory", directory, "--node-id", api.LocalNodeID}, nil, io.Discard); err == nil {
		t.Fatal("local alias was accepted as another machine's actual identity")
	}
	after, err := os.ReadFile(path)
	files, readErr := os.ReadDir(directory)
	if err != nil || readErr != nil || string(before) != string(after) || len(files) != 1 {
		t.Fatal("read-only identity verification changed enrollment", err, readErr)
	}
}

func TestClosedCoordinatorVerificationRejectsUnownedOrUnboundState(t *testing.T) {
	for _, reason := range []string{"missing", "wrong-id", "insecure-file", "insecure-directory", "symlink-file", "symlink-directory", "large", "extra-fields"} {
		t.Run(reason, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "node.json")
			if err := localstate.Write(path, struct {
				ID string `json:"id"`
			}{"expected-node"}); err != nil {
				t.Fatal(err)
			}
			switch reason {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "wrong-id":
				if err := os.WriteFile(path, []byte(`{"id":"other-node"}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "insecure-file":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "insecure-directory":
				if err := os.Chmod(directory, 0755); err != nil {
					t.Fatal(err)
				}
			case "symlink-file":
				target := filepath.Join(directory, "actual.json")
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "symlink-directory":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(directory, link); err != nil {
					t.Fatal(err)
				}
				directory = link
			case "large":
				if err := os.WriteFile(path, []byte(strings.Repeat(" ", 4097)), 0600); err != nil {
					t.Fatal(err)
				}
			case "extra-fields":
				if err := os.WriteFile(path, []byte(`{"id":"expected-node","adopt":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := Run(t.Context(), []string{"verify-join-directory", "--directory", directory, "--node-id", "expected-node"}, nil, io.Discard); err == nil {
				t.Fatal("closed helper adopted invalid coordinator enrollment")
			}
		})
	}
	// Legacy reverse-join permission checks retain their existing behavior.
	if err := Run(t.Context(), []string{"verify-join-directory", "--directory", t.TempDir()}, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
}
