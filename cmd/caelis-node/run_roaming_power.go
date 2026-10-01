package main

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/leasepower"
)

func bindRoamingPower(ctx context.Context, suspend, wake func()) (func(), error) {
	return leasepower.Bind(ctx, suspend, wake)
}
