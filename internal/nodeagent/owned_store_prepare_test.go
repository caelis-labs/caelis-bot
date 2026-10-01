//go:build darwin || linux

package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func TestPrepareOwnedCaelisStoreCLIUsesExactEnrolledNodeAndSafeReceipt(t *testing.T) {
	directory, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(directory, 0700); e != nil {
		t.Fatal(e)
	}
	if e = localstate.Write(filepath.Join(directory, "node.json"), struct {
		ID string `json:"id"`
	}{"enrolled-node"}); e != nil {
		t.Fatal(e)
	}
	var output bytes.Buffer
	args := []string{"prepare-owned-caelis-store", "--directory", directory, "--node-id", "enrolled-node"}
	if e = Run(t.Context(), args, bytes.NewReader(nil), &output); e != nil {
		t.Fatal(e)
	}
	var result OwnedStorePreparation
	if e = json.Unmarshal(output.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if result.NodeID != "enrolled-node" || result.Store != filepath.Join(directory, "caelis-store") || result.NativeHome != filepath.Join(result.Store, ".native-home") || !result.Prepared || !result.AuthenticationRequired {
		t.Fatal("native receipt lost authentication scope", result)
	}
	if eligible, reason := caelis.ProbeOwnedStore(result.NodeID, result.Store); !eligible || reason != "" {
		t.Fatal(eligible, reason)
	}
	if _, e = os.Stat(filepath.Join(result.Store, "runtime")); !os.IsNotExist(e) {
		t.Fatal("preparation started/initialized a Host", e)
	}
	output.Reset()
	if e = Run(t.Context(), args, bytes.NewReader(nil), &output); e == nil || output.Len() != 0 {
		t.Fatal("CLI silently reused prepared Store", e, output.String())
	}
}
func TestPrepareOwnedCaelisStoreCLIRejectsUnenrolledForeignAndCancelledSetup(t *testing.T) {
	for _, scenario := range []string{"unenrolled", "foreign-node", "cancelled", "extra-argument"} {
		t.Run(scenario, func(t *testing.T) {
			directory, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			if e = os.Chmod(directory, 0700); e != nil {
				t.Fatal(e)
			}
			if scenario != "unenrolled" {
				if e = localstate.Write(filepath.Join(directory, "node.json"), struct {
					ID string `json:"id"`
				}{"enrolled-node"}); e != nil {
					t.Fatal(e)
				}
			}
			nodeID := "enrolled-node"
			if scenario == "foreign-node" {
				nodeID = "other-node"
			}
			args := []string{"prepare-owned-caelis-store", "--directory", directory, "--node-id", nodeID}
			if scenario == "extra-argument" {
				args = append(args, "unclosed")
			}
			ctx := t.Context()
			if scenario == "cancelled" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			var out bytes.Buffer
			if e = Run(ctx, args, bytes.NewReader(nil), &out); e == nil || out.Len() != 0 {
				t.Fatal("unsafe setup accepted", e, out.String())
			}
			if _, e = os.Stat(filepath.Join(directory, "caelis-store")); !os.IsNotExist(e) {
				t.Fatal("rejected setup wrote Store", e)
			}
		})
	}
}
