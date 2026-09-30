//go:build !linux

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
	return nil, unavailable("logind sleep fencing is available only on Linux; bind the native platform owner", nil)
}
