package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodes"
)

func TestWorkerNodeBackendsHaveIndependentBindingsAndDetach(t *testing.T) {
	c, registry, _ := nodeFixture(t)
	caelis, codex := nodeConfig(), nodeConfig()
	codex.Backend, codex.Socket = "codex", "/private/worker.sock"
	adapters := map[api.WorkTarget]*nodeTestAdapter{}
	directories := map[api.WorkTarget]string{}
	factoryCalls := 0
	c.factory = func(config backend.WorkerNodeConfig, directory string) (workerNodeAdapter, error) {
		factoryCalls++
		target := configuredWorkerTarget(config)
		adapter := &nodeTestAdapter{worker: &nodeTestWorker{}}
		adapters[target], directories[target] = adapter, directory
		return adapter, nil
	}
	if _, err := c.Save(caelis, c.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	// The legacy action and credential directory remain valid before a second backend exists.
	if _, err := c.Connect(t.Context(), caelis.ID, c.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	caelisTarget, codexTarget := configuredWorkerTarget(caelis), configuredWorkerTarget(codex)
	legacyDirectory := filepath.Join(filepath.Dir(c.path), "worker-nodes", caelis.ID)
	if directories[caelisTarget] != legacyDirectory {
		t.Fatal("legacy credential scope moved")
	}
	marker := filepath.Join(legacyDirectory, "existing-private-binding.json")
	if err := os.WriteFile(marker, []byte("retained-binding"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Save(codex, c.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	if runtime, err := registry.WorkRuntimeFor(caelisTarget); err != nil || runtime != adapters[caelisTarget].worker {
		t.Fatal("saving another backend overwrote the live route", err)
	}
	service := &backend.Service{}
	service.ConfigureWorkerNodes(c)
	revision, calls := c.Snapshot().Revision, factoryCalls
	for _, action := range []func(context.Context, api.WorkTarget, uint64) (backend.WorkerNodeSetup, error){service.ProbeWorkerTarget, service.ConnectWorkerTarget, service.DisconnectWorkerTarget} {
		if snapshot, err := action(t.Context(), api.WorkTarget{NodeID: caelis.ID}, revision); err == nil || snapshot.Revision != revision {
			t.Fatal("ambiguous legacy action mutated setup", snapshot, err)
		}
	}
	if factoryCalls != calls || adapters[caelisTarget].closes != 0 {
		t.Fatal("ambiguous action touched a native connection")
	}
	probed, err := service.ProbeWorkerTarget(t.Context(), codexTarget, revision)
	if err != nil || adapters[codexTarget].probes != 1 || adapters[codexTarget].connects != 0 || adapters[codexTarget].closes != 1 {
		t.Fatal("exact probe enrolled or touched another backend", probed, err)
	}
	if _, err := service.ConnectWorkerTarget(t.Context(), codexTarget, revision); err == nil {
		t.Fatal("exact action ignored the global revision")
	}
	ready, err := service.ConnectWorkerTarget(t.Context(), codexTarget, probed.Revision)
	if err != nil || len(ready.Nodes) != 2 || !ready.Nodes[0].Connected || !ready.Nodes[1].Connected {
		t.Fatal("backends cannot coexist", ready, err)
	}
	if directories[codexTarget] != filepath.Join(filepath.Dir(c.path), "worker-nodes", ".codex", codex.ID) {
		t.Fatal("backend scopes collide")
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "retained-binding" {
		t.Fatal("existing legacy binding was overwritten", err)
	}
	for _, target := range []api.WorkTarget{caelisTarget, codexTarget} {
		if runtime, err := registry.WorkRuntimeFor(target); err != nil || runtime != adapters[target].worker {
			t.Fatal("route does not select exact native backend", target, err)
		}
	}
	// Labels describe the machine; changing one label preserves both execution bindings.
	codex.Label = "Renamed machine"
	if snapshot, err := c.Save(codex, ready.Revision); err != nil || snapshot.Nodes[0].Config.Label != codex.Label || snapshot.Nodes[1].Config.Label != codex.Label || snapshot.Nodes[0].Config.Backend != "" {
		t.Fatal("machine metadata changed backend authority", snapshot, err)
	}
	offlineRegistry, err := nodes.New("offline-fixture", newTestEngine())
	if err != nil {
		t.Fatal(err)
	}
	reloaded := openWorkerNodes(c.path, offlineRegistry, nil)
	if snapshot := reloaded.Snapshot(); snapshot.Issue != "" || len(snapshot.Nodes) != 2 || snapshot.Nodes[0].Config.Backend != "" {
		t.Fatal("offline reload lost a backend or legacy scope", snapshot)
	}
	_ = reloaded.Close()
	if snapshot, err := service.DisconnectWorkerTarget(t.Context(), codexTarget, c.Snapshot().Revision); err != nil || !snapshot.Nodes[0].Connected || snapshot.Nodes[1].Connected {
		t.Fatal("exact detach affected the other backend", snapshot, err)
	}
	if adapters[codexTarget].closes != 1 || adapters[caelisTarget].closes != 0 || adapters[codexTarget].worker.stops != 0 || adapters[caelisTarget].worker.stops != 0 {
		t.Fatal("detach closed or stopped the wrong native work")
	}
	if _, err := registry.WorkRuntimeFor(codexTarget); err == nil {
		t.Fatal("detached backend remains dispatchable")
	}
	if _, err := registry.WorkRuntimeFor(caelisTarget); err != nil {
		t.Fatal("other backend lost readiness", err)
	}
	if _, err := service.ConnectWorkerTarget(t.Context(), codexTarget, c.Snapshot().Revision); err != nil {
		t.Fatal("detached backend cannot reconnect", err)
	}
	if snapshot, err := service.DisconnectWorkerTarget(t.Context(), caelisTarget, c.Snapshot().Revision); err != nil || snapshot.Nodes[0].Connected || !snapshot.Nodes[1].Connected {
		t.Fatal("reverse detach affected the other backend", snapshot, err)
	}
	if _, err := registry.WorkRuntimeFor(codexTarget); err != nil || adapters[codexTarget].closes != 0 || adapters[caelisTarget].closes != 1 {
		t.Fatal("reverse detach lost the other native port", err)
	}
	if _, err := registry.ResolveWorkTarget(nil); err != nil {
		t.Fatal("multi-backend setup affected direct local default", err)
	}
}

func TestWorkerNodeTargetValidationPreservesMachineAndLegacyScope(t *testing.T) {
	c, _, adapter := nodeFixture(t)
	legacy := nodeConfig()
	if _, err := c.Save(legacy, c.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatal(err)
	}
	revision := c.Snapshot().Revision
	for _, config := range []backend.WorkerNodeConfig{
		{ID: legacy.ID, Label: legacy.Label, SSH: legacy.SSH, Backend: "caelis", WorkspaceRoot: legacy.WorkspaceRoot},
		{ID: legacy.ID, Label: legacy.Label, SSH: "other-machine", Backend: "codex", Socket: "/private/worker.sock", WorkspaceRoot: legacy.WorkspaceRoot},
	} {
		if _, err := c.Save(config, revision); err == nil {
			t.Fatal("existing machine or legacy grant was rebound")
		}
	}
	for _, target := range []api.WorkTarget{
		{NodeID: legacy.ID, Backend: "caelis", Role: api.RoleBot},
		{NodeID: legacy.ID, Backend: "unknown", Role: api.RoleWorker},
		{NodeID: "unconfigured", Backend: "codex", Role: api.RoleWorker},
		{NodeID: legacy.ID},
	} {
		for _, action := range []func(context.Context, api.WorkTarget, uint64) (backend.WorkerNodeSetup, error){c.ProbeTarget, c.ConnectTarget, c.DisconnectTarget} {
			if snapshot, err := action(t.Context(), target, revision); err == nil || snapshot.Revision != revision {
				t.Fatal("invalid exact target affected setup", target, snapshot, err)
			}
		}
	}
	if current, err := os.ReadFile(c.path); err != nil || !bytes.Equal(current, original) || adapter.probes+adapter.connects+adapter.closes != 0 {
		t.Fatal("rejected action changed persisted/native state", err)
	}
}

func TestWorkerNodeLoadRejectsDuplicateTargetOrConflictingMachine(t *testing.T) {
	for _, mutation := range []func(*backend.WorkerNodeConfig){
		func(config *backend.WorkerNodeConfig) { config.Backend = "caelis" },
		func(config *backend.WorkerNodeConfig) {
			config.Backend, config.Socket, config.SSH = "codex", "/private/worker.sock", "other-machine"
		},
	} {
		c, registry, _ := nodeFixture(t)
		first, second := nodeConfig(), nodeConfig()
		mutation(&second)
		b, err := json.Marshal(workerNodeDocument{Version: 1, Nodes: []backend.WorkerNodeConfig{first, second}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(c.path, b, 0600); err != nil {
			t.Fatal(err)
		}
		reloaded := openWorkerNodes(c.path, registry, nil)
		if snapshot := reloaded.Snapshot(); snapshot.Issue != "config_unreadable" || len(snapshot.Nodes) != 0 {
			t.Fatal("conflicting machine binding was silently adopted", snapshot)
		}
		_ = reloaded.Close()
		if current, err := os.ReadFile(c.path); err != nil || !bytes.Equal(current, b) {
			t.Fatal("invalid configuration was rewritten", err)
		}
	}
}
