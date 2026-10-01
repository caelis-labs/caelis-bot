package main

import (
	"context"
	"encoding/json"
	"errors"
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
