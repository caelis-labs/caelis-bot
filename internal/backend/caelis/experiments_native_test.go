package caelis

import (
	"context"
	"os"
	"os/exec"
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
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(root, "store")}
	env := newExecutionFixture(t, root)
	dir := filepath.Join(root, "bot")
	var cmd *exec.Cmd
	stop := func() {
		if cmd != nil {
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
			cmd = nil
		}
	}
	defer stop()
	start := func() {
		cmd = exec.CommandContext(ctx, bin, "serve", "--store-dir", settings.CaelisStore, "--listen", "127.0.0.1:0")
		cmd.Dir, cmd.Env = root, env.env
		log, e := os.OpenFile(filepath.Join(root, "host.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		cmd.Stdout, cmd.Stderr = log, log
		e = cmd.Start()
		log.Close()
		if e != nil {
			t.Fatal(e)
		}
		waitAcceptance(t, ctx, func() bool { _, _, e := Discover(settings); return e == nil })
	}
	read := func() api.RuntimeExperiments {
		v, e := ReadRuntimeExperiments(ctx, settings, dir)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	initializeDefaults := func(profile string) {
		c, info, e := experimentalClient(ctx, settings)
		if e != nil {
			t.Fatal(e)
		}
		defer c.http.CloseIdleConnections()
		if e = initializeExperiments(ctx, c, info, profile); e != nil {
			t.Fatal(e)
		}
	}
	start()
	v := read()
	if !v.Supported {
		t.Fatal("pinned Host missing experimental contract")
	}
	for _, f := range v.Features {
		if f.Enabled {
			t.Fatal("read initialized Core defaults", v)
		}
	}
	initializeDefaults(dir)
	v = read()
	for _, f := range v.Features {
		if !f.Enabled {
			t.Fatal("first connection did not enable defaults", v)
		}
	}
	for _, id := range experimentalIDs {
		v, err = ChangeRuntimeExperiments(ctx, settings, dir, api.RuntimeExperimentChange{ID: id, Enabled: false, ExpectedRevision: v.Revision})
		if err != nil || v.SaveState != "saved" {
			t.Fatalf("save %s: %v %+v", id, err, v)
		}
	}
	originalID, revision := v.OperationID, v.Revision
	stop()
	start()
	initializeDefaults(dir)
	initializeDefaults(filepath.Join(root, "second-bot"))
	v = read()
	for _, f := range v.Features {
		if f.Enabled {
			t.Fatal("restart overwrote user choice", v)
		}
	}
	if v.Revision != revision || v.OperationID != originalID {
		t.Fatal("restart wrote a new operation", v)
	}
	t.Log("official Host: read-only entry, three defaults, three independent saves, Host restart and second Bot profile preserved all disabled choices")
}
