package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Explicit, read-only runtime probes for diagnosing the settings waterfall.
// Never runs in ordinary checks or CI. Bot configuration writes are isolated
// in t.TempDir; no prompts, credentials or configuration values are logged.
func TestWindowsSetupInspectionTiming(t *testing.T) {
	root := os.Getenv("CAELIS_BOT_PERF_PROFILE")
	if root == "" {
		t.Skip("set CAELIS_BOT_PERF_PROFILE for live local runtime timing")
	}
	a, err := New(t.TempDir(), Host{})
	if err != nil {
		t.Fatal("could not construct isolated inspection")
	}
	defer a.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	for _, id := range []string{"caelis", "codex"} {
		start := time.Now()
		profile, e := backend.LoadRuntimeSettings(filepath.Join(root, "runtime-profiles", id+".json"), id)
		if e != nil {
			t.Fatal("profile read failed", id)
		}
		t.Logf("%s profile %.1fms", id, float64(time.Since(start).Microseconds())/1000)
		if e = a.setup.saveProfile(profile); e != nil {
			t.Fatal("isolated profile write failed")
		}
		for _, probe := range []struct {
			name string
			run  func() error
		}{
			{"InspectSetup", func() error {
				v, e := a.setup.Inspect(ctx, profile)
				if e == nil {
					t.Logf("%s ready=%t catalog=%d parameterized=%d", id, v.State == "ready", len(v.Models), parameterizedModels(v))
				}
				return e
			}},
			{"SetupModels", func() error { _, e := a.setup.SetupModels(ctx, id); return e }},
		} {
			start = time.Now()
			e := probe.run()
			t.Logf("%s %s %.1fms", id, probe.name, float64(time.Since(start).Microseconds())/1000)
			if e != nil {
				t.Fatal("probe failed", id, probe.name)
			}
		}
	}
}
func parameterizedModels(v api.SetupState) int {
	n := 0
	for _, m := range v.Models {
		if m.Parameters != nil {
			n++
		}
	}
	return n
}
