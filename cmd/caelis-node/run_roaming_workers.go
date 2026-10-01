package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

// The native deployment writes this private manifest from approved enrolled
// pairs. Only Nodes reach product settings; agent sockets remain host-only.
type roamingWorkerPlan struct {
	Version int                        `json:"version"`
	Nodes   []backend.WorkerNodeConfig `json:"nodes"`
	Agents  []roamingWorkerAgent       `json:"agents"`
}
type roamingWorkerAgent struct {
	NodeID  string `json:"nodeId"`
	Backend string `json:"backend"`
	Socket  string `json:"socket"`
}

func (a roamingWorkerAgent) target() api.WorkTarget {
	return api.WorkTarget{NodeID: a.NodeID, Backend: a.Backend, Role: api.RoleWorker}
}
func loadRoamingWorkerPlan(filename string) (roamingWorkerPlan, error) {
	var document roamingWorkerPlan
	if filename == "" {
		return document, nil
	}
	b, err := readBrokerFile(filename, 64<<10)
	if err != nil {
		return document, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&document) != nil || d.Decode(&struct{}{}) != io.EOF || document.Version != 1 || len(document.Nodes) > 16 || len(document.Agents) != len(document.Nodes) {
		return document, errors.New("invalid private enrolled Worker plan")
	}
	seen := map[api.WorkTarget]bool{}
	for _, config := range document.Nodes {
		target := startupTarget(config)
		if target.Validate() != nil || target.NodeID == api.LocalNodeID || seen[target] || config.Transport != "registered-agent" || (target.Backend != "codex" && target.Backend != "caelis") || config.SSH != "" || config.Helper != "" || config.Socket != "" || config.Store != "" || config.WorkspaceRoot != "" {
			return document, errors.New("managed Worker requires exact enrolled node/backend metadata")
		}
		seen[target] = true
	}
	for _, agent := range document.Agents {
		target := agent.target()
		if !seen[target] || !filepath.IsAbs(agent.Socket) || filepath.Clean(agent.Socket) != agent.Socket || len(agent.Socket) >= 100 || strings.ContainsAny(agent.Socket, ":\x00\r\n") {
			return document, errors.New("private Worker agent route does not match approved roster")
		}
		delete(seen, target)
	}
	if len(seen) != 0 {
		return document, errors.New("private Worker route missing")
	}
	return document, nil
}

// Kept for callers inspecting metadata; it never dials or exposes private paths.
func loadRoamingWorkers(filename string) ([]backend.WorkerNodeConfig, error) {
	p, e := loadRoamingWorkerPlan(filename)
	return p.Nodes, e
}

type roamingWorkerAgents struct {
	mu      sync.Mutex
	source  workerwire.Pair
	routes  map[api.WorkTarget]roamingWorkerAgent
	clients map[api.WorkTarget]*nodeagent.Client
	dial    func(context.Context, roamingWorkerAgent) (*nodeagent.Client, error)
}

func newRoamingWorkerAgents(life context.Context, c roamingCommand, plan roamingWorkerPlan) *roamingWorkerAgents {
	r := &roamingWorkerAgents{source: workerwire.Pair{BotID: api.ProfileBotID(c.BotID), SourceNode: c.NodeID, SourceBackend: c.Backend}, routes: map[api.WorkTarget]roamingWorkerAgent{}, clients: map[api.WorkTarget]*nodeagent.Client{}}
	for _, agent := range plan.Agents {
		r.routes[agent.target()] = agent
	}
	r.dial = func(ctx context.Context, agent roamingWorkerAgent) (*nodeagent.Client, error) {
		if c.BrokerSSH.Target != "" {
			return nodeagent.NewSSHClient(life, c.BrokerSSH, c.BrokerHelper, agent.Socket, agent.NodeID)
		}
		return nodeagent.Dial(ctx, agent.Socket, agent.NodeID)
	}
	return r
}
func (r *roamingWorkerAgents) lookup(ctx context.Context, pair workerwire.Pair) (app.RegisteredWorkerAgent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	route, ok := r.routes[pair.Target]
	if !ok || pair.BotID != r.source.BotID || pair.SourceNode != r.source.SourceNode || pair.SourceBackend != r.source.SourceBackend {
		return nil, errors.New("Worker pairing is outside the approved native plan")
	}
	if client := r.clients[pair.Target]; client != nil {
		return client, nil
	}
	client, err := r.dial(ctx, route)
	if err != nil {
		return nil, err
	}
	r.clients[pair.Target] = client
	return client, nil
}
func (r *roamingWorkerAgents) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for target, c := range r.clients {
		c.Close()
		delete(r.clients, target)
	}
}
func connectRoamingWorkers(ctx context.Context, service *backend.Service, configs []backend.WorkerNodeConfig) error {
	selected := make([]api.WorkTarget, 0, len(configs))
	for _, config := range configs {
		if _, err := service.SaveWorkerNode(config, service.WorkerNodes().Revision); err != nil {
			return err
		}
		selected = append(selected, startupTarget(config))
	}
	return connectStartupWorkers(ctx, service, selected)
}
