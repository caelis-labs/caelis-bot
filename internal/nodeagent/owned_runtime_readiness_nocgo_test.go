//go:build darwin && !cgo

package nodeagent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
)

func TestUnsupportedProductionReadinessDoesNotPersistIntentOrLaunch(t *testing.T) {
	service, request, calls := readinessFixture(t)
	service.readinessCheck = nil
	result, err := service.CheckOwnedRuntimeReadiness(t.Context(), request)
	if !errors.Is(err, codex.ErrOwnedRuntimeUnsupported) || result.Ready || result.StopConfirmed || result.Outcome != "unavailable" || result.Reason != "owned-runtime-unsupported" {
		t.Fatal(result, err)
	}
	if calls.Load() != 0 {
		t.Fatal("unsupported readiness launched native check")
	}
	if _, err := os.Stat(filepath.Join(service.options.Directory, "owned-readiness")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsupported readiness persisted mutation intent", err)
	}
}
