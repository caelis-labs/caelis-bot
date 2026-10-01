//go:build (darwin && cgo) || linux

package main

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRoamingAgentOwnsActualLeasedWorkerAcrossObserverDetach(t *testing.T) {
	for _, nodeID := range []string{"worker-node", api.LocalNodeID} {
		t.Run(nodeID, func(t *testing.T) { testRoamingAgentOwnedWorkerDetach(t, nodeID) })
	}
}
