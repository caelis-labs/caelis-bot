package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/app"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

func TestRoamingWorkerPlanPinsExactMetadataAndPrivateAgentRoutes(t *testing.T) {
	valid := func() roamingWorkerPlan {
		return roamingWorkerPlan{Version: 1, Nodes: []backend.WorkerNodeConfig{{ID: "worker-node", Label: "Enrolled Worker", Backend: "codex", Transport: "registered-agent"}}, Agents: []roamingWorkerAgent{{NodeID: "worker-node", Backend: "codex", Socket: "/private/broker/peer/agent.sock"}}}
	}
	filename := filepath.Join(t.TempDir(), "workers.json")
	write := func(plan roamingWorkerPlan) {
		b, _ := json.Marshal(plan)
		if err := os.WriteFile(filename, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan := valid()
	write(plan)
	loaded, err := loadRoamingWorkerPlan(filename)
	if err != nil || len(loaded.Nodes) != 1 || loaded.Nodes[0].Socket != "" || loaded.Agents[0].Socket != plan.Agents[0].Socket {
		t.Fatalf("approved private roster %+v %v", loaded, err)
	}
	for _, mutate := range []func(*roamingWorkerPlan){
		func(p *roamingWorkerPlan) { p.Agents = nil },
		func(p *roamingWorkerPlan) { p.Agents[0].NodeID = "foreign-node" },
		func(p *roamingWorkerPlan) { p.Agents[0].Backend = "caelis" },
		func(p *roamingWorkerPlan) { p.Nodes[0].SSH = "operator-host" },
		func(p *roamingWorkerPlan) { p.Nodes[0].Socket = "/private/runtime.sock" },
		func(p *roamingWorkerPlan) { p.Nodes[0].WorkspaceRoot = "/private/workspace" },
		func(p *roamingWorkerPlan) { p.Nodes[0].Transport = "" },
		func(p *roamingWorkerPlan) { p.Agents[0].Socket = "https://host/agent" },
		func(p *roamingWorkerPlan) {
			p.Nodes = append(p.Nodes, p.Nodes[0])
			p.Agents = append(p.Agents, p.Agents[0])
		},
	} {
		plan = valid()
		mutate(&plan)
		write(plan)
		if _, err := loadRoamingWorkerPlan(filename); err == nil {
			t.Fatalf("unapproved route accepted %+v", plan)
		}
	}
	plan = valid()
	plan.Nodes[0].Backend = "caelis"
	plan.Agents[0].Backend = "caelis"
	write(plan)
	if _, err := loadRoamingWorkerPlan(filename); err != nil {
		t.Fatalf("exact owned Caelis Worker refused: %v", err)
	}
}
func TestRoamingWorkerLookupRejectsChangedSourceBeforeDial(t *testing.T) {
	config := roamingCommand{NodeID: "owner-node", BotID: "native-bot", Backend: "codex"}
	route := roamingWorkerAgent{NodeID: "worker-node", Backend: "codex", Socket: "/private/broker/peer/agent.sock"}
	lookup := newRoamingWorkerAgents(t.Context(), config, roamingWorkerPlan{Agents: []roamingWorkerAgent{route}})
	calls := 0
	sentinel := errors.New("contained unavailable agent")
	lookup.dial = func(_ context.Context, actual roamingWorkerAgent) (*nodeagent.Client, error) {
		calls++
		if actual != route {
			t.Fatalf("private route substituted %+v", actual)
		}
		return nil, sentinel
	}
	pair := workerwire.Pair{Target: route.target(), BotID: api.ProfileBotID(config.BotID), SourceNode: config.NodeID, SourceBackend: config.Backend}
	for _, mutate := range []func(*workerwire.Pair){func(p *workerwire.Pair) { p.SourceNode = "foreign-node" }, func(p *workerwire.Pair) { p.BotID = "foreign-bot" }, func(p *workerwire.Pair) { p.SourceBackend = "caelis" }, func(p *workerwire.Pair) { p.Target.Backend = "caelis" }, func(p *workerwire.Pair) { p.Target.Role = api.RoleBot }} {
		changed := pair
		mutate(&changed)
		if _, err := lookup.lookup(t.Context(), changed); err == nil || calls != 0 {
			t.Fatalf("changed pairing opened route %+v %v", changed, err)
		}
	}
	if _, err := lookup.lookup(t.Context(), pair); !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("approved pairing did not resolve exact private route: %v calls=%d", err, calls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := lookup.lookup(ctx, pair); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancelled lookup opened new agent")
	}
}

func TestRoamingWorkerPlanSeparatesSourcesAndBothTargetBindings(t *testing.T) {
	p := roamingWorkerPlan{Version: 1, Sources: []roamingWorkerSource{{NodeID: "same-node", Backends: []string{"codex", "caelis"}}}, Runtimes: []roamingWorkerRuntime{{Backend: "codex", Binary: "/native/codex", Model: "fixture-model", Execution: &api.WorkExecutionSettings{Model: "fixture-model", Effort: "medium"}}, {Backend: "caelis", Binary: "/native/caelis", Store: "/native/caelis-store", Model: "fixture-model"}}}
	filename := filepath.Join(t.TempDir(), "workers.json")
	check := func(valid bool) {
		t.Helper()
		b, _ := json.Marshal(p)
		if err := os.WriteFile(filename, b, 0600); err != nil {
			t.Fatal(err)
		}
		_, err := loadRoamingWorkerPlan(filename)
		if (err == nil) != valid {
			t.Fatalf("binding validation valid=%t err=%v plan=%+v", valid, err, p)
		}
	}
	check(true)
	p.Sources[0].Backends = []string{"codex", "unknown"}
	check(false)
	p.Sources[0].Backends = []string{"codex", "codex"}
	check(false)
	p.Sources[0].Backends = []string{"codex", "caelis"}
	p.Runtimes[1].Backend = "codex"
	check(false)
	p.Runtimes[1].Backend = "caelis"
	p.Runtimes[1].Store = "relative-store"
	check(false)
	p.Runtimes[1].Store = "/native/caelis-store"
	p.Runtimes[0].Binary = "relative-binary"
	check(false)
}

type roamingOptionalWorkerFixture struct{ *startupFixture }

func (s roamingOptionalWorkerFixture) SaveWorkerNode(config backend.WorkerNodeConfig, revision uint64) (backend.WorkerNodeSetup, error) {
	return s.snapshot, nil
}
func TestRoamingUnavailableOptionalWorkerPreservesPrimaryStartup(t *testing.T) {
	s := startupFixtureFor()
	s.prepared = true
	s.failAt = 1
	s.failure = errors.New("target owned Host unavailable")
	configs := []backend.WorkerNodeConfig{s.snapshot.Nodes[0].Config, s.snapshot.Nodes[1].Config}
	if err := connectRoamingWorkers(t.Context(), roamingOptionalWorkerFixture{s}, configs); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 2 || !s.snapshot.Nodes[1].Connected || s.snapshot.Nodes[0].Connected {
		t.Fatalf("optional handshake prevented healthy route %+v", s)
	}
}

func TestProductionLocalWorkerManifestPreservesBothBackendsAndNativeCLIIdentity(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "workers.json")
	plan := roamingWorkerPlan{Version: 1,
		Nodes: []backend.WorkerNodeConfig{
			{ID: api.LocalNodeID, Label: "Mac", Backend: "codex", Transport: "registered-agent"},
			{ID: api.LocalNodeID, Label: "Mac", Backend: "caelis", Transport: "registered-agent"},
		},
		Agents: []roamingWorkerAgent{
			{NodeID: api.LocalNodeID, Backend: "codex", Socket: "/private/mac/agent.sock"},
			{NodeID: api.LocalNodeID, Backend: "caelis", Socket: "/private/mac/agent.sock"},
		},
		Sources:  []roamingWorkerSource{{NodeID: "linux-primary", Backends: []string{"codex", "caelis"}}, {NodeID: api.LocalNodeID, Backends: []string{"codex", "caelis"}}},
		Runtimes: []roamingWorkerRuntime{{Backend: "codex", Binary: "/native/codex"}, {Backend: "caelis", Binary: "/native/caelis", Store: "/native/caelis-store"}},
	}
	data, _ := json.Marshal(plan)
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadRoamingWorkerPlan(filename)
	if err != nil || loaded.Nodes[0].ID != api.LocalNodeID || loaded.Agents[1].NodeID != api.LocalNodeID || len(loaded.Runtimes) != 2 {
		t.Fatal("production manifest rejected or renamed local", loaded, err)
	}
	privateConfig, _ := json.Marshal(struct {
		Version int                        `json:"version"`
		Nodes   []backend.WorkerNodeConfig `json:"nodes"`
	}{Version: 1, Nodes: loaded.Nodes})
	if err := os.WriteFile(filepath.Join(root, "worker-nodes.json"), privateConfig, 0600); err != nil {
		t.Fatal(err)
	}
	if err := app.ValidateConfiguredWorkerTargets(root, []api.WorkTarget{loaded.Agents[0].target(), loaded.Agents[1].target()}); err != nil {
		t.Fatal("native APP preflight rejected production manifest", err)
	}
	for _, nativeBackend := range []string{"codex", "caelis"} {
		args := []string{"serve-roaming", "--node-id", "linux-primary", "--bot-id", "native-bot", "--broker-node-id", api.LocalNodeID, "--agent-directory", "/private/linux-agent", "--generations", "/private/generations", "--broker-socket", "/private/broker.sock", "--auth-file", "/private/auth", "--workers-file", filename, "--backend", nativeBackend}
		if nativeBackend == "caelis" {
			args = append(args, "--caelis-binary", "/native/caelis", "--caelis-store", "/native/caelis-store")
		}
		command, err := parseRoamingCommand(args, io.Discard)
		if err != nil || command.BrokerNodeID != api.LocalNodeID || command.NodeID != "linux-primary" {
			t.Fatal(command, err)
		}
		lookup := newRoamingWorkerAgents(t.Context(), command, loaded)
		calls := 0
		sentinel := errors.New("contained unavailable Mac agent")
		lookup.dial = func(_ context.Context, agent roamingWorkerAgent) (*nodeagent.Client, error) {
			calls++
			if agent.NodeID != api.LocalNodeID || agent.Socket != "/private/mac/agent.sock" {
				t.Fatal("native route renamed", agent)
			}
			return nil, sentinel
		}
		for _, agent := range loaded.Agents {
			pair := workerwire.Pair{Target: agent.target(), BotID: api.ProfileBotID(command.BotID), SourceNode: command.NodeID, SourceBackend: nativeBackend}
			if _, err := lookup.lookup(t.Context(), pair); !errors.Is(err, sentinel) {
				t.Fatal("exact local route unavailable to native lookup", pair, err)
			}
			wrong := pair
			wrong.SourceNode = api.LocalNodeID
			before := calls
			if _, err := lookup.lookup(t.Context(), wrong); err == nil || calls != before {
				t.Fatal("APP alias replaced actual source", wrong, err)
			}
		}
	}
	// Matching local fields never authorize legacy SSH transport or paths.
	plan.Nodes[0].Transport = ""
	plan.Nodes[0].SSH = "foreign-host"
	data, _ = json.Marshal(plan)
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRoamingWorkerPlan(filename); err == nil {
		t.Fatal("legacy local SSH route admitted")
	}
}
