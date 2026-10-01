package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
)

func TestNativeNodeCaelisPrivateSlotDoesNotChangeOrdinaryLocalProfile(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(dir, "app")
	binary := filepath.Join(dir, "caelis")
	helper := filepath.Join(dir, "native-helper")
	for _, path := range []string{binary, helper} {
		if e = os.WriteFile(path, []byte("#!/bin/sh\nprintf '{\"version\":\"0.1.0\"}\\n'\n"), 0700); e != nil {
			t.Fatal(e)
		}
	}
	if e = localstate.Write(filepath.Join(root, "runtime.json"), api.RuntimeSettings{Runtime: "codex", CLIPath: "/source/codex"}); e != nil {
		t.Fatal(e)
	}
	if e = localstate.Write(filepath.Join(root, "runtime-profiles", "caelis.json"), struct {
		Version int `json:"version"`
		api.RuntimeSettings
	}{1, api.RuntimeSettings{Runtime: "caelis", CLIPath: binary}}); e != nil {
		t.Fatal(e)
	}
	a, e := New(root, Host{})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	before, e := a.Backend.SetupProfile("caelis")
	if e != nil {
		t.Fatal(e)
	}
	sourceBefore := a.Backend.RuntimeSettings()
	port, e := backend.NativeNodeRoamingController(a.Backend)
	if e != nil {
		t.Fatal(e)
	}
	roaming := port.(*nodeRoamingControl)
	if e = AttachNodeManagement(a, NodeManagementNativeOptions{JoinHelperPath: helper}); e != nil {
		t.Fatal(e)
	}
	management, e := backend.NativeNodeManagementController(a.Backend)
	if e != nil {
		t.Fatal(e)
	}
	controller := management.(*nodeManagement).agent.(*nativeNodeManagement)
	service, ok := controller.local.(*nodeagent.Service)
	if !ok {
		t.Fatal("real local node agent missing")
	}
	metadata, e := service.ReadOwnedRuntimeSettings(t.Context(), api.LocalNodeID, api.NodeCaelis)
	expectedStore := filepath.Join(root, "nodeplane", "local", "caelis-store")
	if e != nil || metadata.Binary != binary || metadata.Store != expectedStore {
		t.Fatal("local private node designation mismatch", metadata, e)
	}
	settings, e := nodeLocalCaelisSettings(a, filepath.Join(root, "nodeplane", "local"), nil)
	if e != nil || settings.CaelisStore != metadata.Store {
		t.Fatal("SDK local designation disagrees with metadata", settings, e)
	}
	assembly := &roamingNativeAssembly{app: a}
	alternative, e := assembly.runtimeSettings(t.Context(), NodeRegistration{ID: api.LocalNodeID}, api.NodeCaelis)
	if e != nil || alternative.CLIPath != binary || alternative.CaelisStore != metadata.Store {
		t.Fatal("roaming alternate chose a different local SDK slot", alternative, e)
	}
	companion, e := controller.localOwnedRuntimeCompanion(t.Context())
	if e != nil || companion.Path != helper || companion.SHA256 == "" {
		t.Fatal("trusted companion missing", companion, e)
	}
	after, e := a.Backend.SetupProfile("caelis")
	if e != nil || after != before || a.Backend.RuntimeSettings() != sourceBefore || a.sourceRetired || a.started {
		t.Fatal("passive node slot changed ordinary profile/source", before, after, e)
	}
	if _, e = os.Lstat(expectedStore); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("passive native metadata created Caelis Store", e)
	}
	explicit := settings
	explicit.CaelisStore = filepath.Join(dir, "explicit-unmarked")
	retained, e := nodeLocalCaelisSettings(a, "/different/node-slot", &explicit)
	if e != nil || retained != explicit {
		t.Fatal("explicit private profile silently replaced", retained, e)
	}
	if _, e = os.Lstat(explicit.CaelisStore); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("explicit unmarked profile implicitly initialized", e)
	}
	// A restored Notebook generation has no alternate provider profile. Keep
	// the exact original machine slot rather than selecting its empty profile.
	fresh, e := New(filepath.Join(dir, "fresh-generation"), Host{})
	if e != nil {
		t.Fatal(e)
	}
	defer fresh.Close()
	if e = a.Backend.ConfigureNodeRoaming(roaming); e != nil {
		t.Fatal(e)
	}
	roaming.local = fresh
	roaming.doc.LocalGenerationDirectory = fresh.root
	retainedMetadata, e := service.ReadOwnedRuntimeSettings(t.Context(), api.LocalNodeID, api.NodeCaelis)
	if e != nil || retainedMetadata != metadata {
		t.Fatal("fresh generation lost original node setup designation", retainedMetadata, e)
	}
	alternative, e = assembly.runtimeSettings(t.Context(), NodeRegistration{ID: api.LocalNodeID}, api.NodeCaelis)
	if e != nil || alternative.CLIPath != metadata.Binary || alternative.CaelisStore != metadata.Store {
		t.Fatal("restored alternate Runtime replaced original slot", alternative, e)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e = controller.localOwnedRuntimeSettings(cancelled, api.NodeCaelis); e == nil {
		t.Fatal("cancelled metadata operation admitted")
	}
}
