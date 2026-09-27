package desktop

import (
	"context"
	"errors"
	"testing"
)

func TestScreenPermissionWaitsForNativeConsent(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
		failed bool
	}{{1, "authorized", false}, {2, "settingsRequired", false}, {3, "", true}, {4, "", true}} {
		polls := 0
		result, err := waitScreenPermission(t.Context(), func() (int, int64) {
			polls++
			if polls == 1 {
				return 0, 0
			}
			return tc.status, -3801
		})
		if polls != 2 || result.State != tc.want || (err != nil) != tc.failed {
			t.Fatalf("status=%d result=%+v polls=%d err=%v", tc.status, result, polls, err)
		}
	}
}
func TestScreenPermissionCancellationDoesNotBecomeGrant(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	polls := 0
	result, err := waitScreenPermission(ctx, func() (int, int64) { polls++; cancel(); return 0, 0 })
	if !errors.Is(err, context.Canceled) || result.State != "" || polls != 1 {
		t.Fatal(result, err, polls)
	}
}
