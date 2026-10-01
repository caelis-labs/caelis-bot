package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"regexp"
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
	Version          int                        `json:"version"`
	Nodes            []backend.WorkerNodeConfig `json:"nodes"`
	Agents           []roamingWorkerAgent       `json:"agents"`
	Sources          []roamingWorkerSource      `json:"sources,omitempty"`
	Runtimes         []roamingWorkerRuntime     `json:"runtimes,omitempty"`
	SettingsRuntimes []roamingWorkerRuntime     `json:"settingsRuntimes,omitempty"`
}

// Sources identify approved primary owners independently of Worker backends.
// Runtimes are the target node's private installed bindings, never renderer data.
type roamingWorkerSource struct {
	NodeID   string   `json:"nodeId"`
	Backends []string `json:"backends"`
}
type roamingWorkerRuntime struct {
	Backend   string                     `json:"backend"`
	Binary    string                     `json:"binary"`
	Store     string                     `json:"store,omitempty"`
	Model     string                     `json:"model,omitempty"`
	Execution *api.WorkExecutionSettings `json:"execution,omitempty"`
}

var roamingSourceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func validRoamingBackend(backend string) bool { return backend == "codex" || backend == "caelis" }

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
	if d.Decode(&document) != nil || d.Decode(&struct{}{}) != io.EOF || document.Version != 1 || len(document.Nodes) > 16 || len(document.Agents) != len(document.Nodes) || len(document.Sources) > 16 || len(document.Runtimes) > 2 || len(document.SettingsRuntimes) > 2 {
		return document, errors.New("invalid private enrolled Worker plan")
	}
	seen := map[api.WorkTarget]bool{}
	for _, config := range document.Nodes {
		target := startupTarget(config)
		if target.Validate() != nil || seen[target] || config.Transport != "registered-agent" || (target.Backend != "codex" && target.Backend != "caelis") || config.SSH != "" || config.Helper != "" || config.Socket != "" || config.Store != "" || config.WorkspaceRoot != "" {
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
	sources := map[string]bool{}
	for _, source := range document.Sources {
		if !roamingSourceID.MatchString(source.NodeID) || sources[source.NodeID] || len(source.Backends) == 0 || len(source.Backends) > 2 {
			return document, errors.New("invalid approved primary source set")
		}
		sources[source.NodeID] = true
		seen := map[string]bool{}
		for _, backend := range source.Backends {
			if !validRoamingBackend(backend) || seen[backend] {
				return document, errors.New("invalid approved primary backend set")
			}
			seen[backend] = true
		}
	}
	for _, bindings := range [][]roamingWorkerRuntime{document.Runtimes, document.SettingsRuntimes} {
		runtimes := map[string]bool{}
		for _, native := range bindings {
			if !validRoamingBackend(native.Backend) || runtimes[native.Backend] || !filepath.IsAbs(native.Binary) || filepath.Clean(native.Binary) != native.Binary || strings.ContainsAny(native.Binary, "\x00\r\n") {
				return document, errors.New("invalid private target Runtime binding")
			}
			runtimes[native.Backend] = true
			if native.Backend == "codex" && native.Store != "" || native.Backend == "caelis" && (!filepath.IsAbs(native.Store) || filepath.Clean(native.Store) != native.Store || strings.ContainsAny(native.Store, "\x00\r\n")) || len(native.Model) > 256 || strings.ContainsAny(native.Model, "\x00\r\n") {
				return document, errors.New("private target Runtime scope changed")
			}
			if native.Execution != nil {
				if err := api.ValidateExecutionSettings(native.Execution.Execution()); err != nil {
					return document, err
				}
			}
		}
	}
	if document.SettingsRuntimes != nil {
		for _, admitted := range document.Runtimes {
			matched := false
			for _, settings := range document.SettingsRuntimes {
				if settings.Backend == admitted.Backend {
					matched = settings.Binary == admitted.Binary && settings.Store == admitted.Store
				}
			}
			if !matched {
				return document, errors.New("admitted Runtime differs from frozen target settings")
			}
		}
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

type roamingWorkerService interface {
	startupWorkers
	SaveWorkerNode(backend.WorkerNodeConfig, uint64) (backend.WorkerNodeSetup, error)
}

func connectRoamingWorkers(ctx context.Context, service roamingWorkerService, configs []backend.WorkerNodeConfig) error {
	selected := make([]api.WorkTarget, 0, len(configs))
	for _, config := range configs {
		if _, err := service.SaveWorkerNode(config, service.WorkerNodes().Revision); err != nil {
			return err
		}
		selected = append(selected, startupTarget(config))
	}
	// Approved bindings still need a live owned/authenticated handshake. A
	// unavailable optional Worker records its native controller issue without
	// taking the primary Bot's otherwise valid generation offline.
	for _, target := range selected {
		if err := connectStartupWorkers(ctx, service, []api.WorkTarget{target}); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return ctx.Err()
}
