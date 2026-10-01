//go:build !darwin && !linux

package main

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

type roamingOwnedWorkers struct{}

func newRoamingOwnedWorkers(context.Context, roamingCommand, roamingWorkerPlan, string, nodeplane.WorkLeaseReader, func(context.Context, func(), func()) (func(), error)) *roamingOwnedWorkers {
	return &roamingOwnedWorkers{}
}
func (*roamingOwnedWorkers) resolve(context.Context, workerwire.Pair) (nodeagent.NativeWorkerProxyEndpoint, error) {
	return nodeagent.NativeWorkerProxyEndpoint{}, errors.New("independent owned Worker supervision unavailable on this platform")
}
func (*roamingOwnedWorkers) Close() error { return nil }

func (*roamingOwnedWorkers) CaelisStoreInUse() bool { return false }
