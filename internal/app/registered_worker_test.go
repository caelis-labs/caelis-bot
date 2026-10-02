package app

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodes"
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
	"io"
)

type registeredTestAgent struct {
	owner *wireNodeFixture
	t     *testing.T
}

func (a registeredTestAgent) OpenWorkerStream(ctx context.Context, pair workerwire.Pair) (io.ReadWriteCloser, error) {
	server, err := workerwire.NewServer(nodeworker.New(a.owner), pair)
	if err != nil {
		return nil, err
	}
	left, right := net.Pipe()
	go func() { _ = server.Serve(a.t.Context(), right) }()
	return left, nil
}

func TestRegisteredWorkerRejectsMachinePathsAndUntrustedEnrollment(t *testing.T) {
	config := backend.WorkerNodeConfig{ID: "enrolled-worker", Label: "Enrolled worker", Backend: "codex", Transport: "registered-agent"}
	if err := validateWorkerNode(config); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*backend.WorkerNodeConfig){
		func(c *backend.WorkerNodeConfig) { c.SSH = "foreign-host" },
		func(c *backend.WorkerNodeConfig) { c.Socket = "/private/foreign.sock" },
		func(c *backend.WorkerNodeConfig) { c.Store = "/private/store" },
		func(c *backend.WorkerNodeConfig) { c.WorkspaceRoot = "/tmp/work" },
		func(c *backend.WorkerNodeConfig) { c.Helper = "foreign-helper" },
	} {
		bad := config
		mutate(&bad)
		if validateWorkerNode(bad) == nil {
			t.Fatal("enrolled topology accepted renderer machine authority")
		}
	}
	primary := &nodePrimarySource{testEngine: newTestEngine()}
	a := &Application{engine: primary, residentNodeID: "actual-source", companion: registeredBot(t)}
	if _, err := a.newWorkerNodeAdapter(config, t.TempDir()); err == nil {
		t.Fatal("untrusted registered route dispatched")
	}
	pair, err := a.workerSourcePair(configuredWorkerTarget(config))
	if err != nil || pair.SourceNode != "actual-source" || pair.BotID != api.ProfileBotID("stable-bot") {
		t.Fatal("pre-start resident identity changed", pair, err)
	}
}

func TestRegisteredWorkerProbeAndDetachDoNotStopOwner(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pair := workerwire.Pair{Target: api.WorkTarget{NodeID: "enrolled-worker", Backend: "codex", Role: api.RoleWorker}, BotID: api.ProfileBotID("stable-bot"), SourceNode: "actual-source", SourceBackend: "fixture"}
	owner := &wireNodeFixture{pair: pair, root: root, revision: 1, changed: make(chan struct{}), tasks: map[string]api.Task{}, intents: map[string]api.WorkStart{}}
	adapter := &registeredWorkerAdapter{pair: pair, source: &nodePrimarySource{testEngine: newTestEngine()}, lookup: func(_ context.Context, actual workerwire.Pair) (RegisteredWorkerAgent, error) {
		if actual != pair {
			t.Fatal("registered route changed")
		}
		return registeredTestAgent{owner: owner, t: t}, nil
	}}
	if _, err := adapter.Probe(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.starts != 0 || owner.stops != 0 || owner.closes != 0 {
		t.Fatal("observer acquired native task/stop authority", owner.starts, owner.stops, owner.closes)
	}
}

func TestRegisteredWorkerLocalMeansExplicitMacAndCannotReplaceNativeDefault(t *testing.T) {
	primary := &nodePrimarySource{testEngine: newTestEngine()}
	a := &Application{engine: primary, residentNodeID: "managed-linux", companion: registeredBot(t)}
	if err := a.ConfigureRegisteredWorkers(func(context.Context, workerwire.Pair) (RegisteredWorkerAgent, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	for _, target := range []api.WorkTarget{
		{NodeID: api.LocalNodeID, Backend: "codex", Role: api.RoleWorker},
		{NodeID: "managed-linux", Backend: "caelis", Role: api.RoleWorker},
	} {
		config := backend.WorkerNodeConfig{ID: target.NodeID, Label: "Explicit Worker", Backend: target.Backend, Transport: "registered-agent"}
		if err := validateWorkerNode(config); err != nil {
			t.Fatal(err)
		}
		adapter, err := a.newRegisteredWorkerNode(config, primary)
		if err != nil {
			t.Fatal("distinct machine or backend rejected", target, err)
		}
		if pair := adapter.(*registeredWorkerAdapter).pair; pair.Target != target || pair.SourceNode != "managed-linux" {
			t.Fatal(pair)
		}
	}
	// Fixture native provider is called fixture; exact backend matching owns
	// self rejection independently of the UI's codex/Caelis configuration check.
	if _, err := a.newRegisteredWorkerNode(backend.WorkerNodeConfig{ID: "managed-linux", Label: "Self", Backend: primary.ProviderInfo().ID, Transport: "registered-agent"}, primary); err == nil {
		t.Fatal("source's own native Worker registered as remote")
	}
	legacy := nodeConfig()
	legacy.ID = api.LocalNodeID
	if validateWorkerNode(legacy) == nil {
		t.Fatal("legacy SSH association overwrote local identity")
	}
	registry, err := nodes.NewAt(nodes.Node{ID: "managed-linux", Label: "Linux"}, "codex", primary)
	if err != nil {
		t.Fatal(err)
	}
	controller := openWorkerNodes(filepath.Join(t.TempDir(), "worker-nodes.json"), registry, nil)
	defer controller.Close()
	self := backend.WorkerNodeConfig{ID: "managed-linux", Label: "Self", Backend: "codex", Transport: "registered-agent"}
	if _, err := controller.Save(self, controller.Snapshot().Revision); err == nil {
		t.Fatal("candidate overwrote native default")
	}
	mac := self
	mac.ID = api.LocalNodeID
	if _, err := controller.Save(mac, controller.Snapshot().Revision); err != nil {
		t.Fatal("explicit enrolled Mac rejected", err)
	}
	if got, err := registry.ResolveWorkTarget(nil); err != nil || got.NodeID != "managed-linux" {
		t.Fatal("enrolled Mac changed managed default", got, err)
	}
}

// Native ports are fixtures; transport, controller and coordinator routing are
// production paths. No model, SSH host or installed runtime is contacted.
type registeredOwnedPrimary struct {
	*nodePrimarySource
	starts int
}

func (p *registeredOwnedPrimary) StartWork(_ context.Context, in api.WorkStart) (api.Task, error) {
	p.starts++
	return api.Task{ID: in.ID, Title: in.Title, Workspace: in.Workspace, Status: "working", Outcome: "accepted"}, nil
}

type registeredLeasedWorkerFixture struct{ *wireNodeFixture }

func (registeredLeasedWorkerFixture) LeaseAwareAdmission() bool { return true }

type registeredLeasedAgentFixture struct {
	owner *wireNodeFixture
	t     *testing.T
}

func (a registeredLeasedAgentFixture) OpenWorkerStream(_ context.Context, pair workerwire.Pair) (io.ReadWriteCloser, error) {
	server, err := workerwire.NewServer(nodeworker.New(registeredLeasedWorkerFixture{a.owner}), pair)
	if err != nil {
		return nil, err
	}
	left, right := net.Pipe()
	go func() { _ = server.Serve(a.t.Context(), right) }()
	return left, nil
}

func TestResidentRegisteredMacWorkerStreamKeepsActualSourceAndIndependentDefault(t *testing.T) {
	root := t.TempDir()
	primary := &registeredOwnedPrimary{nodePrimarySource: &nodePrimarySource{testEngine: newTestEngine(), value: api.WorkDispatchSource{NodeID: "managed-linux", Backend: "fixture", BindingID: "original-thread", OperationID: "original-turn", Kind: "native_activation"}}}
	macRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mac := &wireNodeFixture{root: macRoot, revision: 1, changed: make(chan struct{}), tasks: map[string]api.Task{}, intents: map[string]api.WorkStart{}}
	a := &Application{root: root, engine: primary, residentNodeID: "managed-linux", companion: registeredBot(t)}
	if err := a.ConfigureRegisteredWorkers(func(_ context.Context, pair workerwire.Pair) (RegisteredWorkerAgent, error) {
		if pair.SourceNode != "managed-linux" || pair.BotID != api.ProfileBotID("stable-bot") || pair.Target.NodeID != api.LocalNodeID {
			t.Fatal("route changed", pair)
		}
		mac.mu.Lock()
		if mac.pair.BotID == "" {
			mac.pair = pair
		} else if mac.pair != pair {
			t.Error("reconnect changed pairing")
		}
		mac.mu.Unlock()
		return registeredLeasedAgentFixture{owner: mac, t: t}, nil
	}); err != nil {
		t.Fatal(err)
	}
	registry, err := nodes.NewAt(nodes.Node{ID: "managed-linux", Label: "Linux"}, "fixture", primary)
	if err != nil {
		t.Fatal(err)
	}
	controller := openWorkerNodes(filepath.Join(root, "worker-nodes.json"), registry, a.newWorkerNodeAdapter)
	defer controller.Close()
	config := backend.WorkerNodeConfig{ID: api.LocalNodeID, Label: "Mac", Backend: "codex", Transport: "registered-agent"}
	saved, err := controller.Save(config, controller.Snapshot().Revision)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := controller.ConnectTarget(t.Context(), configuredWorkerTarget(config), saved.Revision)
	if err != nil || ready.Nodes[0].State != "ready" {
		t.Fatal(ready, err)
	}
	manager, err := tasks.OpenRouted(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "fixture", primary, primary, primary.Snapshot, registry, primary)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := manager.StartTask(t.Context(), api.TaskStart{RequestID: "managed-direct-wire", Title: "Direct", Prompt: "Fixture task"})
	if err != nil || direct.Target == nil || *direct.Target != registry.DefaultTarget() || primary.starts != 1 {
		t.Fatal(direct, err)
	}
	target := configuredWorkerTarget(config)
	request := api.TaskStart{RequestID: "explicit-mac-wire", Title: "Mac", Prompt: "Fixture task", Target: &target}
	work, err := manager.StartTask(t.Context(), request)
	if err != nil || work.Target == nil || *work.Target != target || filepath.Dir(work.Workspace) != macRoot {
		t.Fatal(work, err)
	}
	mac.mu.Lock()
	original := mac.intents[work.ID]
	starts := mac.starts
	mac.mu.Unlock()
	if starts != 1 || primary.starts != 1 || original.Source != primary.value || original.RequestDigest == "" {
		t.Fatal("wrong native identity", original)
	}
	if _, err := controller.DisconnectTarget(t.Context(), target, ready.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReadTask(t.Context(), work.ID); err == nil {
		t.Fatal("detached Mac task rerouted")
	}
	if got, err := manager.StartTask(t.Context(), request); err != nil || got.ID != work.ID || primary.starts != 1 {
		t.Fatal("original receipt replayed", got, err)
	}
	if got, err := registry.ResolveWorkTarget(nil); err != nil || got.NodeID != "managed-linux" {
		t.Fatal("Mac detach removed direct default", got, err)
	}
	mac.mu.Lock()
	defer mac.mu.Unlock()
	if mac.starts != 1 || mac.stops != 0 || mac.closes != 0 {
		t.Fatal("observer detach mutated owner", mac.starts, mac.stops, mac.closes)
	}
}

func registeredBot(t *testing.T) *bot.Runtime {
	t.Helper()
	p := filepath.Join(t.TempDir(), "bot.json")
	if err := localstate.Write(p, bot.State{Version: 1, ID: "stable-bot"}); err != nil {
		t.Fatal(err)
	}
	r, err := bot.New(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
