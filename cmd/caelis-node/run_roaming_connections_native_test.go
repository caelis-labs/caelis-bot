//go:build (darwin && cgo) || linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/memorytransfer"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

func TestRoamingNodeWizardBorrowsExactWarmHostWithoutStoppingBot(t *testing.T) {
	root := canonicalWorkerTestRoot(t)
	helper, e := verifiedRoamingExecutable()
	if e != nil {
		t.Fatal(e)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	binary := filepath.Join(root, "caelis")
	if e = os.WriteFile(binary, []byte("#!/bin/sh\nexec "+quote(helper)+" -test.run='^TestRoamingCaelisWorkerNativeHelper$' -- \"$@\"\n"), 0700); e != nil {
		t.Fatal(e)
	}
	payload, ref, e := memorytransfer.EncodeNotebook(t.Context(), memorytransfer.NotebookSnapshot{BotID: "roaming-fixture", Epoch: "0", Version: "1", Files: []memorytransfer.NotebookFile{
		{File: memorytransfer.File{Path: "bot.json"}, Body: []byte(`{"version":1,"personalVersion":1,"id":"roaming-fixture","schedules":[]}`)},
		{File: memorytransfer.File{Path: "notebook-migration.json"}, Body: []byte(`{"version":1}`)},
		{File: memorytransfer.File{Path: "bot-initialization.json"}, Body: []byte(`{"version":1,"id":"accepted-intro","status":"accepted"}`)},
		{File: memorytransfer.File{Path: "Notebook/MEMORY.md"}, Body: []byte("# Memory\nOwned setup fixture.\n")},
	}})
	if e != nil {
		t.Fatal(e)
	}
	generation := filepath.Join(root, "generation")
	_, e = memorytransfer.ApplyNotebook(t.Context(), memorytransfer.NotebookApplyOptions{Payload: payload, Destination: generation, DestinationStopped: true, Expected: ref, Commit: func(_ context.Context, got nodeplane.SnapshotRef, install func() error) error {
		if got != ref {
			return errors.New("fixture snapshot changed")
		}
		return install()
	}})
	if e != nil {
		t.Fatal(e)
	}
	store := filepath.Join(root, "owned-store")
	target := api.WorkTarget{NodeID: "owned-node", Backend: "caelis", Role: api.RoleBot}
	native, _, e := app.NewManagedNode(generation, app.Host{BindLeasePower: func(func(), func()) (func(), error) { return func() {}, nil }}, target.NodeID, app.ManagedNodeOptions{Backend: api.NodeCaelis, BrokerNodeID: "paired-broker", WatchdogHelperPath: helper, CaelisHost: &caelis.OwnedHostOptions{NodeID: target.NodeID, Binary: binary, Store: store}, Model: "fixture-model"})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := native.Close(); e != nil {
			t.Error(e)
		}
	}()
	h := &roamingProofOwner{target: target}
	if e = h.register(target, native); e != nil {
		t.Fatal(e)
	}
	metadata := nodeagent.OwnedRuntimeSettings{Backend: api.NodeCaelis, Binary: binary, Store: store}
	owner, e := h.nodeConnectionOwner(t.Context(), metadata)
	if e != nil || owner == nil {
		t.Fatal("warm owned Host unavailable to wizard", e)
	}
	settings, e := owner.SetupSettings(t.Context())
	if e != nil || settings.CLIPath != binary || settings.CaelisStore != store {
		t.Fatal("warm native SDK designation changed", settings, e)
	}
	pidBytes, e := os.ReadFile(filepath.Join(store, "fixture-pid"))
	if e != nil {
		t.Fatal(e)
	}
	pid, e := strconv.Atoi(string(pidBytes))
	if e != nil {
		t.Fatal(e)
	}
	wrong := metadata
	wrong.Store = filepath.Join(root, "foreign-store")
	if _, e = h.nodeConnectionOwner(t.Context(), wrong); e == nil {
		t.Fatal("warm scope retargeted")
	}
	if _, e = app.NativeOwnedCaelisSetupSettings(t.Context(), native, "foreign-node"); e == nil {
		t.Fatal("another node borrowed Host")
	}
	if e = owner.Close(t.Context()); e != nil {
		t.Fatal(e)
	}
	if e = syscall.Kill(pid, 0); e != nil {
		t.Fatal("wizard close stopped active Host", e)
	}
	if e = owner.Check(t.Context()); e == nil {
		t.Fatal("closed observer still authorized")
	}
	second, e := h.nodeConnectionOwner(t.Context(), metadata)
	if e != nil {
		t.Fatal(e)
	}
	h.clear()
	if e = second.Check(t.Context()); e == nil {
		t.Fatal("stale generation borrowed authority")
	}
	if e = second.Close(t.Context()); e != nil {
		t.Fatal(e)
	}
	if e = syscall.Kill(pid, 0); e != nil {
		t.Fatal("stale observer close stopped Bot Host", e)
	}
	for _, name := range []string{"worker-enrolled", "worker-prompted", "unexpected-call"} {
		if _, e = os.Lstat(filepath.Join(store, name)); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("wizard performed application/model work", name, e)
		}
	}
	if e = native.Close(); e != nil {
		t.Fatal(e)
	}
	if e = syscall.Kill(pid, 0); !errors.Is(e, syscall.ESRCH) {
		t.Fatal("actual native owner did not confirm stop", e)
	}
}
