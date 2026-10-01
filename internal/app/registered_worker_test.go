package app

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
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
	a := &Application{engine: primary, managed: &managedNodeOwner{nodeID: "actual-source", snapshot: nodeplane.SnapshotRef{BotID: "stable-bot"}}}
	if _, err := a.newWorkerNodeAdapter(config, t.TempDir()); err == nil {
		t.Fatal("untrusted registered route dispatched")
	}
	pair, err := a.workerSourcePair(configuredWorkerTarget(config))
	if err != nil || pair.SourceNode != "actual-source" || pair.BotID != api.ProfileBotID("stable-bot") {
		t.Fatal("pre-start managed identity changed", pair, err)
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
