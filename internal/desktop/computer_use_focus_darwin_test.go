//go:build darwin && cgo

package desktop

import (
	"context"
	"errors"
	"testing"
)

func TestComputerUseFocusRejectsCancellationAndInvalidNativeIdentities(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := FocusComputerUseWindow(ctx, 1, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled focus reached the native route", err)
	}
	for _, input := range []struct {
		pid    int
		window uint64
	}{{0, 1}, {-1, 1}, {1, 0}, {1 << 31, 1}, {1, 1 << 32}} {
		if err := FocusComputerUseWindow(t.Context(), input.pid, input.window); err == nil {
			t.Fatal("invalid native identity accepted")
		}
	}
}
