package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
)

// Normal setup writes this source profile; no hand-written runtime.json exists.
// The real Notebook production controller then invokes the candidate native
// helper for prepare, including its existing-profile refresh/preferences branch.
// Runtime setup speaks only to a contained synthetic stdio CLI; no model runs.
func TestNotebookPrepareUsesNormalSetupProfile(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_COMPOSITION_HELPER")
	if helper == "" {
		t.Skip("requires explicit candidate native helper; no real nodes")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("contained setup fixture needs Python")
	}
	root, err := os.MkdirTemp("/tmp", "cb-schema-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("HOME", root)
	t.Setenv("CODEX_HOME", filepath.Join(root, "empty-codex-home"))
	t.Setenv("CODEX_BIN", "")
	fixture, err := os.ReadFile("../nodeagent/testdata/codex_setup_fixture.py")
	if err != nil {
		t.Fatal(err)
	}
	fixture = bytes.Replace(fixture, []byte("#!/usr/bin/env python3"), []byte("#!"+python), 1)
	// A synthetic account-presence projection suffices for normal setup; this
	// fixture has no native credentials or account file to open or transfer.
	fixture = bytes.Replace(fixture, []byte("{'type': 'apiKey', 'apiKey': 'NATIVE_ONLY_FIXTURE_SECRET'}"), []byte("{'type': 'chatgpt'}"), 1)
	binary := filepath.Join(root, "codex-fixture")
	if err = os.WriteFile(binary, fixture, 0700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(root, "source")
	if err = os.Mkdir(profile, 0700); err != nil {
		t.Fatal(err)
	}
	source, err := New(profile, Host{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	settings := api.RuntimeSettings{Runtime: "codex", CLIPath: binary}
	if err = source.Backend.ActivateRuntime(ctx, settings); err != nil {
		t.Fatal("normal setup activation", err)
	}
	var normal backend.RuntimeDocument
	b, err := os.ReadFile(filepath.Join(profile, "runtime.json"))
	if err != nil || json.Unmarshal(b, &normal) != nil || normal.Version != 1 || normal.RuntimeSettings != settings {
		t.Fatal("normal setup did not write complete canonical v1", err)
	}
	// Reopen the actual normal persisted profile through production assembly.
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	source, err = New(profile, Host{})
	if err != nil {
		t.Fatal("normal profile reopen", err)
	}
	directory := filepath.Join(root, "node")
	if err = os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	const targetID = "NODE-SCHEMA"
	if _, err = nodeagent.New(nodeagent.Options{Directory: directory, NodeID: targetID, Join: api.NodeSSH}); err != nil {
		t.Fatal(err)
	}
	if err = localstate.Write(filepath.Join(directory, "runtime-codex.json"), settings); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(root, "agent")
	if err = os.WriteFile(agent, []byte("#!/bin/sh\nexec "+gateQuote(helper)+" serve-agent --stdio --directory "+gateQuote(directory)+" --node-id "+gateQuote(targetID)+" --native-health=false\n"), 0700); err != nil {
		t.Fatal(err)
	}
	tools := filepath.Join(root, "bin")
	if err = os.Mkdir(tools, 0700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = -G ]; then printf 'hostname fixture\\nuser fixture\\nport 22\\nuserknownhostsfile /dev/null\\nglobalknownhostsfile /dev/null\\n'; exit 0; fi\nwhile [ \"$#\" -gt 0 ]; do case \"$1\" in -o) shift 2;; -T) shift;; --) shift; break;; *) break;; esac; done\n[ \"$1\" = fixture ] || exit 2\nshift\nexec /bin/sh -c \"$*\"\n"
	if err = os.WriteFile(filepath.Join(tools, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+":/usr/bin:/bin")
	controller, err := backend.NativeNodeManagementController(source.Backend)
	if err != nil {
		t.Fatal(err)
	}
	native := controller.(*nodeManagement).agent.(*nativeNodeManagement)
	native.document.Nodes = []NodeRegistration{{ID: targetID, Label: "Contained standby", Join: api.NodeSSH, SSHDestination: "fixture", Directory: directory, HelperPath: agent, HostHelperPath: helper}}
	if err = writeNodeManagementDocument(filepath.Join(native.directory, "config.json"), native.document); err != nil {
		t.Fatal(err)
	}
	for _, interval := range []int{1, 2} {
		saved, err := source.Backend.SaveNotebookSyncSettings(ctx, backend.NotebookSyncSettings{Enabled: true, IntervalMinutes: interval, Targets: []backend.NotebookBackupTarget{{NodeID: targetID, Backend: api.NodeCodex}}})
		if err != nil || !saved.Enabled || saved.SourceBackend != api.NodeCodex {
			t.Fatal("actual Notebook prepare/refresh from setup profile", saved, err)
		}
	}
	standby := nodeagent.NotebookOwnerProfile(directory)
	loaded, err := ReadNotebookRuntimeSettings(standby)
	if err != nil || loaded != settings {
		t.Fatal("prepared canonical target Runtime unreadable", err)
	}
	b, err = os.ReadFile(filepath.Join(standby, "runtime.json"))
	if err != nil || json.Unmarshal(b, &normal) != nil || normal.Version != 1 {
		t.Fatal("prepare stripped normal version", err)
	}
	if _, err = os.Stat(filepath.Join(standby, "Notebook")); err != nil {
		t.Fatal("actual standby not prepared", err)
	}
	if _, err = os.Stat(filepath.Join(standby, "conversation.json")); !os.IsNotExist(err) {
		t.Fatal("prepare created an old session")
	}
	owned, err := NewOwnedResident(ctx, standby, Host{}, targetID, helper)
	if err != nil {
		t.Fatal("normal owned constructor rejected prepared metadata", err)
	}
	if err = owned.Close(); err != nil {
		t.Fatal(err)
	}
	t.Log("normal ActivateRuntime -> canonical v1 source reopen -> real native Notebook prepare + existing standby refresh -> owned profile construction passed; no model or Runtime owner started")
}

func TestNotebookRuntimeDocumentRetainsStrictValidation(t *testing.T) {
	profile := t.TempDir()
	value := api.RuntimeSettings{Runtime: "caelis", CLIPath: "/fixture/caelis", CaelisStore: "/fixture/store"}
	path := filepath.Join(profile, "runtime.json")
	if err := backend.SaveRuntimeSettingsDocument(path, value); err != nil {
		t.Fatal(err)
	}
	if loaded, err := ReadNotebookRuntimeSettings(profile); err != nil || loaded != value {
		t.Fatal("canonical source/standby Runtime rejected", err)
	}
	for _, mutate := range []func(map[string]any){func(v map[string]any) { v["version"] = 2 }, func(v map[string]any) { v["unknown"] = true }, func(v map[string]any) { v["runtime"] = "invalid/runtime" }} {
		if err := backend.SaveRuntimeSettingsDocument(path, value); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(path)
		var doc map[string]any
		_ = json.Unmarshal(b, &doc)
		mutate(doc)
		b, _ = json.Marshal(doc)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadNotebookRuntimeSettings(profile); err == nil {
			t.Fatal("invalid document accepted")
		}
	}
	if err := backend.SaveRuntimeSettingsDocument(path, value); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadNotebookRuntimeSettings(profile); err == nil {
		t.Fatal("nonprivate Runtime configuration accepted")
	}
}
