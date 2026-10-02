//go:build !linux && (!darwin || !cgo)

package leasepower

import "context"

func markCloseOnExec(int) {}

func BindWithOptions(_ context.Context, suspend, wake func(), opts Options) (func(), error) {
	if _, err := checkedOptions(suspend, wake, opts); err != nil {
		if suspend != nil {
			suspend()
		}
		return nil, err
	}
	suspend()
	return nil, unavailable("native power fencing requires Linux logind or a Darwin build with cgo", nil)
}
