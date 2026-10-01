//go:build !linux && (!darwin || !cgo)

package leasepower

import (
	"errors"
	"strings"
	"testing"
)

func TestUnsupportedNativePowerDisablesManagedEligibility(t *testing.T) {
	fenced := false
	release, err := Bind(t.Context(), func() { fenced = true }, func() { t.Error("unsupported binding restored ownership") })
	var reason *UnavailableError
	if release != nil || !fenced || !errors.Is(err, ErrUnavailable) || !errors.As(err, &reason) || !strings.Contains(reason.Reason, "Darwin build with cgo") {
		t.Fatal("unsupported native power did not supply an ineligible reason", err)
	}
}
