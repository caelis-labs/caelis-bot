// Package nodes binds execution capabilities to machines. It does not own
// credentials, SSH transport, native Host identity or process lifecycle.
package nodes

import (
	"errors"
	"sort"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type Node struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	OS    string `json:"os,omitempty"`
}

type Availability string

const (
	Candidate   Availability = "candidate"
	Ready       Availability = "ready"
	Unavailable Availability = "unavailable"
)

type Capability struct {
	Target api.WorkTarget `json:"target"`
	State  Availability   `json:"state"`
}

type entry struct {
	Capability
	runtime api.WorkRuntime
}

type Registry struct {
	mu      sync.RWMutex
	local   api.WorkTarget
	nodes   map[string]Node
	entries map[api.WorkTarget]entry
}

// New retains the direct in-process local Worker as the default. Construction
// has no network, daemon, enrollment or runtime-discovery prerequisites.
func New(backend string, local api.WorkRuntime) (*Registry, error) {
	if backend == "" || local == nil {
		return nil, errors.New("local worker configuration is incomplete")
	}
	target := api.WorkTarget{NodeID: api.LocalNodeID, Backend: backend, Role: api.RoleWorker}
	r := &Registry{local: target, nodes: map[string]Node{}, entries: map[api.WorkTarget]entry{}}
	if err := r.Set(Node{ID: api.LocalNodeID, Label: "This machine"}, Capability{Target: target, State: Ready}, local); err != nil {
		return nil, err
	}
	return r, nil
}

// Set is a trusted assembly operation, after target identity/capability checks.
// A candidate can be displayed without falsely advertising ready execution.
// Unavailable capabilities retain machine/target identity but cannot dispatch.
func (r *Registry) Set(node Node, capability Capability, runtime api.WorkRuntime) error {
	if err := capability.Target.Validate(); err != nil {
		return err
	}
	if node.ID == "" || node.ID != capability.Target.NodeID || node.Label == "" {
		return errors.New("node identity does not match execution capability")
	}
	if capability.State != Candidate && capability.State != Ready && capability.State != Unavailable {
		return errors.New("unknown capability availability")
	}
	if (capability.State == Ready) != (runtime != nil) {
		return errors.New("only a ready capability may bind an execution port")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[node.ID] = node
	r.entries[capability.Target] = entry{Capability: capability, runtime: runtime}
	return nil
}

func (r *Registry) ResolveWorkTarget(requested *api.WorkTarget) (api.WorkTarget, error) {
	target := r.local
	if requested != nil {
		target = *requested
	}
	_, err := r.WorkRuntimeFor(target)
	return target, err
}

func (r *Registry) WorkRuntimeFor(target api.WorkTarget) (api.WorkRuntime, error) {
	if err := target.Validate(); err != nil {
		return nil, err
	}
	if target.Role != api.RoleWorker {
		return nil, errors.New("task dispatch requires the worker role")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[target]
	if !ok || e.State != Ready || e.runtime == nil {
		return nil, errors.New("selected worker target is unavailable")
	}
	return e.runtime, nil
}

func (r *Registry) WorkRoutes() []api.WorkRoute {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []api.WorkRoute
	for target, e := range r.entries {
		if target.Role == api.RoleWorker && e.State == Ready && e.runtime != nil {
			out = append(out, api.WorkRoute{Target: target, Runtime: e.runtime})
		}
	}
	sort.Slice(out, func(i, j int) bool { return targetKey(out[i].Target) < targetKey(out[j].Target) })
	return out
}

func (r *Registry) Capabilities() []Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Capability
	for _, e := range r.entries {
		out = append(out, e.Capability)
	}
	sort.Slice(out, func(i, j int) bool { return targetKey(out[i].Target) < targetKey(out[j].Target) })
	return out
}

func (r *Registry) Nodes() []Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Node
	for _, node := range r.nodes {
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *Registry) WorkTargets() []api.WorkTargetInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []api.WorkTargetInfo
	for target, entry := range r.entries {
		if target.Role == api.RoleWorker {
			out = append(out, api.WorkTargetInfo{Target: target, Label: r.nodes[target.NodeID].Label, State: string(entry.State)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return targetKey(out[i].Target) < targetKey(out[j].Target) })
	return out
}

func targetKey(t api.WorkTarget) string {
	return t.NodeID + "\x00" + t.Backend + "\x00" + string(t.Role)
}

var _ api.WorkRouter = (*Registry)(nil)
