package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodes"
)

type nodeTestWorker struct {
	api.WorkRuntime
	stops int
}

type nodeReadinessWorker struct {
	*nodeTestWorker
	configured bool
	auth       string
}

func (w *nodeReadinessWorker) ModelReadiness() (bool, string) { return w.configured, w.auth }

type nodeReadinessAdapter struct {
	*nodeTestAdapter
	ready *nodeReadinessWorker
}

func (a *nodeReadinessAdapter) Connect(context.Context) (api.WorkRuntime, error) {
	a.connects++
	return a.ready, a.err
}

func (w *nodeTestWorker) StopWork(context.Context, string) (api.Task, error) {
	w.stops++
	return api.Task{}, nil
}

type nodeTestAdapter struct {
	worker                   *nodeTestWorker
	probes, connects, closes int
	err                      error
}

func (a *nodeTestAdapter) Probe(context.Context) (backend.WorkerNodeFacts, error) {
	a.probes++
	return backend.WorkerNodeFacts{OS: "linux", Arch: "arm64", Version: "fixture"}, a.err
}
func (a *nodeTestAdapter) Connect(context.Context) (api.WorkRuntime, error) {
	a.connects++
	return a.worker, a.err
}
func (a *nodeTestAdapter) Close(context.Context) error { a.closes++; return nil }

func nodeFixture(t *testing.T) (*workerNodeController, *nodes.Registry, *nodeTestAdapter) {
	t.Helper()
	registry, err := nodes.New("fixture", newTestEngine())
	if err != nil {
		t.Fatal(err)
	}
	adapter := &nodeTestAdapter{worker: &nodeTestWorker{}}
	controller := openWorkerNodes(filepath.Join(t.TempDir(), "worker-nodes.json"), registry, func(config backend.WorkerNodeConfig, directory string) (workerNodeAdapter, error) {
		if config.ID != "rocky" || filepath.Base(directory) != "rocky" {
			t.Error("wrong native connection ownership")
		}
		return adapter, nil
	})
	t.Cleanup(func() { _ = controller.Close() })
	return controller, registry, adapter
}

func nodeConfig() backend.WorkerNodeConfig {
	return backend.WorkerNodeConfig{ID: "rocky", Label: "Test Worker", SSH: "fixture-host", WorkspaceRoot: "/tmp/owned-test-work"}
}

func TestWorkerNodesLoadAndSaveOfflineWithoutRemotePrerequisites(t *testing.T) {
	c, registry, adapter := nodeFixture(t)
	saved, err := c.Save(nodeConfig(), c.Snapshot().Revision)
	if err != nil || saved.Nodes[0].State != "candidate" {
		t.Fatal(saved, err)
	}
	if adapter.probes+adapter.connects != 0 {
		t.Fatal("saving a candidate performed network or enrollment")
	}
	if _, err = registry.ResolveWorkTarget(nil); err != nil {
		t.Fatal("optional node affected default local worker", err)
	}
	target := workerTarget("rocky")
	if _, err = registry.ResolveWorkTarget(&target); err == nil {
		t.Fatal("candidate advertised execution readiness")
	}
	info, err := os.Stat(c.path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("connection settings are not private", err)
	}
	reloaded := openWorkerNodes(c.path, registry, func(backend.WorkerNodeConfig, string) (workerNodeAdapter, error) {
		t.Fatal("offline construction called remote factory")
		return nil, nil
	})
	defer reloaded.Close()
	if reloaded.Snapshot().Nodes[0].State != "candidate" {
		t.Fatal("reloaded config became connected implicitly")
	}
}

func TestCodexWorkerConfigRequiresExplicitNativeScopeAndKeepsLegacyDefault(t *testing.T) {
	legacy := nodeConfig()
	if configuredWorkerTarget(legacy) != workerTarget(legacy.ID) {
		t.Fatal("old Caelis configuration changed backend")
	}
	legacyController, _, _ := nodeFixture(t)
	if _, err := legacyController.Save(legacy, legacyController.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	upgraded := legacy
	upgraded.Backend = "caelis"
	if _, err := legacyController.Save(upgraded, legacyController.Snapshot().Revision); err == nil {
		t.Fatal("legacy shared credentials silently changed protocol")
	}
	c, registry, _ := nodeFixture(t)
	config := legacy
	config.Backend = "codex"
	config.Socket = "/private/worker.sock"
	if _, err := c.Save(config, c.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	target := configuredWorkerTarget(config)
	if target.Backend != "codex" {
		t.Fatal("Codex native route mislabeled")
	}
	if _, err := registry.WorkRuntimeFor(target); err == nil {
		t.Fatal("saved candidate dispatched")
	}
	for _, change := range []func(*backend.WorkerNodeConfig){func(c *backend.WorkerNodeConfig) { c.Socket = "" }, func(c *backend.WorkerNodeConfig) { c.Backend = "unknown" }, func(c *backend.WorkerNodeConfig) { c.Store = "/unrelated/caelis" }, func(c *backend.WorkerNodeConfig) { c.Socket = "../worker.sock" }} {
		bad := config
		change(&bad)
		if validateWorkerNode(bad) == nil {
			t.Fatal("invalid native Codex configuration accepted", bad)
		}
	}
	config.Backend = "caelis"
	if validateWorkerNode(config) == nil {
		t.Fatal("Caelis route adopted a Codex native socket")
	}
	changed := legacy
	changed.Backend = "codex"
	changed.Socket = "/different/socket"
	if _, err := c.Save(changed, c.Snapshot().Revision); err == nil {
		t.Fatal("configured target changed connection identity")
	}
}

func TestWorkerNodeProbeConnectAndDetachHaveDifferentAuthority(t *testing.T) {
	c, registry, adapter := nodeFixture(t)
	saved, err := c.Save(nodeConfig(), c.Snapshot().Revision)
	if err != nil {
		t.Fatal(err)
	}
	probed, err := c.Probe(t.Context(), "rocky", saved.Revision)
	if err != nil || probed.Nodes[0].Facts.OS != "linux" || probed.Nodes[0].State != "candidate" || adapter.probes != 1 || adapter.connects != 0 {
		t.Fatal("probe enrolled or became ready", probed, err)
	}
	if _, err = c.Connect(t.Context(), "rocky", saved.Revision); err == nil || adapter.connects != 0 {
		t.Fatal("stale setup action used the updated connection")
	}
	ready, err := c.Connect(t.Context(), "rocky", probed.Revision)
	if err != nil || ready.Nodes[0].State != "ready" || adapter.connects != 1 {
		t.Fatal(ready, err)
	}
	target := workerTarget("rocky")
	if runtime, err := registry.WorkRuntimeFor(target); err != nil || runtime != adapter.worker {
		t.Fatal("ready route not bound to negotiated native port", err)
	}
	detached, err := c.Disconnect(t.Context(), "rocky", ready.Revision)
	if err != nil || detached.Nodes[0].Issue != "detached" || adapter.worker.stops != 0 || adapter.closes != 2 {
		t.Fatal("detach cancelled worker or failed to close owned observation", detached, err)
	}
	if _, err = registry.WorkRuntimeFor(target); err == nil {
		t.Fatal("detached connection remains dispatchable")
	}
	if _, err = registry.ResolveWorkTarget(nil); err != nil {
		t.Fatal("detach blocked local work", err)
	}
	changed := nodeConfig()
	changed.SSH = "another-host"
	if _, err = c.Save(changed, detached.Revision); err == nil {
		t.Fatal("existing task target could silently move machines")
	}
}

func TestWorkerNodeFailureIsSanitizedAndDoesNotFallback(t *testing.T) {
	c, registry, adapter := nodeFixture(t)
	adapter.err = errors.New("secret credential and private ssh endpoint must never reach renderer")
	saved, err := c.Save(nodeConfig(), c.Snapshot().Revision)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := c.Connect(t.Context(), "rocky", saved.Revision)
	if err == nil || strings.Contains(err.Error(), "secret") || failed.Nodes[0].State != "unavailable" {
		t.Fatal("unsafe failure projection", failed, err)
	}
	target := workerTarget("rocky")
	if _, err = registry.ResolveWorkTarget(&target); err == nil {
		t.Fatal("unreachable remote silently fell back")
	}
	if _, err = registry.ResolveWorkTarget(nil); err != nil {
		t.Fatal("remote error blocked local mode", err)
	}
}

func TestWorkerNodeCorruptConfigAndUnsafeBindingDirectoryPreserved(t *testing.T) {
	c, registry, _ := nodeFixture(t)
	bad := []byte(`{"version":1,"nodes":[{"id":"local","token":"private"}]}`)
	if err := os.WriteFile(c.path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	broken := openWorkerNodes(c.path, registry, nil)
	defer broken.Close()
	if broken.Snapshot().Issue != "config_unreadable" {
		t.Fatal("corrupt optional config accepted")
	}
	if _, err := broken.Save(nodeConfig(), broken.Snapshot().Revision); err == nil {
		t.Fatal("corrupt file silently overwritten")
	}
	if b, _ := os.ReadFile(c.path); string(b) != string(bad) {
		t.Fatal("original optional state was lost")
	}
	if _, err := registry.ResolveWorkTarget(nil); err != nil {
		t.Fatal("corrupt remote state blocked default local", err)
	}
	root := t.TempDir()
	out := t.TempDir()
	if err := os.Symlink(out, filepath.Join(root, "worker-nodes")); err != nil {
		t.Fatal(err)
	}
	if err := privateWorkerDirectory(filepath.Join(root, "worker-nodes", "rocky")); err == nil {
		t.Fatal("native binding followed symlink into unrelated directory")
	}
}

func TestWorkerTransportDoesNotAdvertiseMissingModelOrAuthenticationAsReady(t *testing.T) {
	for _, state := range []struct {
		configured  bool
		auth, issue string
	}{{false, "unknown", "model_setup_required"}, {true, "reported_missing", "authentication_required"}} {
		t.Run(state.issue, func(t *testing.T) {
			c, registry, adapter := nodeFixture(t)
			c.factory = func(backend.WorkerNodeConfig, string) (workerNodeAdapter, error) {
				return &nodeReadinessAdapter{nodeTestAdapter: adapter, ready: &nodeReadinessWorker{nodeTestWorker: adapter.worker, configured: state.configured, auth: state.auth}}, nil
			}
			saved, err := c.Save(nodeConfig(), c.Snapshot().Revision)
			if err != nil {
				t.Fatal(err)
			}
			connected, err := c.Connect(t.Context(), "rocky", saved.Revision)
			if err != nil || !connected.Nodes[0].Connected || connected.Nodes[0].State != "candidate" || connected.Nodes[0].Issue != state.issue {
				t.Fatal(connected, err)
			}
			if _, err = registry.WorkRuntimeFor(workerTarget("rocky")); err == nil {
				t.Fatal("missing model or auth advertised as dispatchable")
			}
			if _, err = registry.ResolveWorkTarget(nil); err != nil {
				t.Fatal("target setup blocked local", err)
			}
		})
	}
}

type nodeSourceEngine struct {
	*testEngine
	sources int
}

func (e *nodeSourceEngine) WorkDispatchSource(context.Context) (api.WorkDispatchSource, error) {
	e.sources++
	return api.WorkDispatchSource{}, errors.New("no active native invocation")
}

func TestApplicationIgnoresUnreadableOptionalNodesAndKeepsDirectLocalAssembly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "runtime.json"), []byte(`{"runtime":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "worker-nodes.json"), []byte(`{"unknown":"preserve"}`), 0600); err != nil {
		t.Fatal(err)
	}
	e := &nodeSourceEngine{testEngine: newTestEngine()}
	a, err := newApplication(root, Host{}, func(id string) (providerFactory, error) {
		return providerFactory{ID: id, Open: func(providerConfig) (api.Engine, error) { return e, nil }}, nil
	})
	if err != nil {
		t.Fatal("optional node file blocked local assembly", err)
	}
	defer a.Close()
	if a.Backend.WorkerNodes().Issue != "config_unreadable" || e.sources != 0 {
		t.Fatal("construction contacted or authorized remote")
	}
	local, err := a.nodeRegistry.ResolveWorkTarget(nil)
	if err != nil || local != (api.WorkTarget{NodeID: api.LocalNodeID, Backend: "fixture", Role: api.RoleWorker}) {
		t.Fatal("direct default local target changed", local, err)
	}
	port, err := a.nodeRegistry.WorkRuntimeFor(local)
	if err != nil || port != e {
		t.Fatal("default route no longer uses the in-process native worker", err)
	}
	if a.started || e.connected != 0 {
		t.Fatal("offline settings started runtime")
	}
}

func TestWorkerNodeBackendSelectionPreservesLegacyScope(t *testing.T) {
	c, registry, adapter := nodeFixture(t)
	saved, err := c.Save(nodeConfig(), c.Snapshot().Revision)
	if err != nil {
		t.Fatal(err)
	}
	config := saved.Nodes[0].Config
	config.Backend = "caelis"
	before, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Save(config, saved.Revision); err == nil {
		t.Fatal("existing shared profile silently changed application scope")
	}
	after, err := os.ReadFile(c.path)
	if err != nil || string(after) != string(before) || c.Snapshot().Revision != saved.Revision {
		t.Fatal("rejected selection changed persistent identity", err)
	}
	config.ID = "new-bounded"
	bounded, err := c.Save(config, saved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if adapter.probes+adapter.connects != 0 {
		t.Fatal("native selection performed implicit enrollment")
	}
	reloaded := openWorkerNodes(c.path, registry, nil)
	defer reloaded.Close()
	got := reloaded.Snapshot()
	if got.Issue != "" || len(got.Nodes) != 2 || got.Nodes[0].Config.Backend != "" || got.Nodes[1].Config.Backend != "caelis" {
		t.Fatal("legacy/explicit scope did not survive offline reload", got)
	}
	for _, node := range bounded.Nodes {
		target := workerTarget(node.Config.ID)
		if _, err = registry.ResolveWorkTarget(&target); err == nil {
			t.Fatal("saved selection became dispatchable without connect")
		}
	}
	for _, unsupported := range []string{"codex", "unknown"} {
		bad := config
		bad.ID, bad.Backend = "unsupported", unsupported
		if _, err = c.Save(bad, bounded.Revision); err == nil {
			t.Fatal("unsupported backend accepted", unsupported)
		}
	}
}
