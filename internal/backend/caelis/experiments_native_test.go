package caelis

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Uses the externally supplied, pinned CLI. No provider, account or model call.
func TestNativeHostExperimentalPreferences(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY for isolated Host acceptance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	hostFixture := newNativeHostFixture(t, bin)
	root, settings := hostFixture.Root, hostFixture.settings
	env := newExecutionFixture(t, root)
	dir := filepath.Join(root, "bot")
	hostFixture.Env = env.env
	read := func() api.RuntimeExperiments {
		v, e := ReadRuntimeExperiments(ctx, settings, dir)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	hostFixture.start(t, ctx)
	v := read()
	if !v.Supported {
		t.Fatal("pinned Host missing experimental contract")
	}
	for _, f := range v.Features {
		if f.Enabled {
			t.Fatal("read initialized Core defaults", v)
		}
	}
	if err := ProbeBinding(ctx, settings, dir); err != nil {
		t.Fatal(err)
	}
	for _, id := range experimentalIDs {
		next, err := ChangeRuntimeExperiments(ctx, settings, dir, api.RuntimeExperimentChange{ID: id, Enabled: true, ExpectedRevision: v.Revision})
		if err != nil || next.SaveState != "saved" {
			t.Fatal("explicit enable failed", next, err)
		}
		v = next
	}
	for _, id := range experimentalIDs {
		next, err := ChangeRuntimeExperiments(ctx, settings, dir, api.RuntimeExperimentChange{ID: id, Enabled: false, ExpectedRevision: v.Revision})
		if err != nil || next.SaveState != "saved" {
			t.Fatalf("save %s: %v %+v", id, err, next)
		}
		v = next
	}
	originalID, revision := v.OperationID, v.Revision
	hostFixture.stopForeground(t)
	hostFixture.start(t, ctx)
	for _, profile := range []string{dir, filepath.Join(root, "second-bot")} {
		if err := ProbeBinding(ctx, settings, profile); err != nil {
			t.Fatal(err)
		}
		current, e := ReadRuntimeExperiments(ctx, settings, profile)
		if e != nil || current.Revision != revision {
			t.Fatal("fresh profile changed shared configuration", current, e)
		}
	}
	v = read()
	for _, f := range v.Features {
		if f.Enabled {
			t.Fatal("restart overwrote user choice", v)
		}
	}
	if v.Revision != revision || v.OperationID != originalID {
		t.Fatal("restart wrote a new operation", v)
	}
	t.Log("official Host: Core defaults unchanged, six explicit independent saves, Host restart and second Bot profile preserved all disabled choices")
}
