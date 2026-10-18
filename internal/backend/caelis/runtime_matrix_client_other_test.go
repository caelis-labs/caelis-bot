//go:build !windows

package caelis

import (
	"context"
	"testing"
)

// The existing macOS matrix keeps its original scope. Native persistent TUI
// observation is an additional Windows acceptance gate, not a cross-build claim.
func observeMatrixClient(_ *testing.T, _ context.Context, _ *client, _, _, _, _ string) func(string) {
	return func(string) {}
}
