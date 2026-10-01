//go:build darwin && cgo

package desktop

import (
	"context"

	"github.com/caelis-labs/caelis-bot/internal/leasepower"
)

// BindManagedLeasePower is opt-in for a leased managed runtime. Default local
// execution never registers this additional system-power owner.
func BindManagedLeasePower(suspend func(), wake func()) (func(), error) {
	return leasepower.Bind(context.Background(), suspend, wake)
}
