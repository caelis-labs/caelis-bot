package app

import (
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

// RegisteredWorkerAgent is an already inspected native pairing. Its private
// transport stays outside the renderer, portable Notebook and model contracts.
type RegisteredWorkerAgent interface {
	OpenWorkerStream(context.Context, workerwire.Pair) (io.ReadWriteCloser, error)
}

type RegisteredWorkerAgentLookup func(context.Context, workerwire.Pair) (RegisteredWorkerAgent, error)

// ConfigureRegisteredWorkers attaches trusted topology before native Start.
// Merely loading a profile or viewing a node never opens a Worker stream.
func (a *Application) ConfigureRegisteredWorkers(lookup RegisteredWorkerAgentLookup) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started || a.closed || lookup == nil {
		return errors.New("registered Workers require native assembly before startup")
	}
	a.registeredWorkers = lookup
	return nil
}

func (a *Application) workerSourcePair(target api.WorkTarget) (workerwire.Pair, error) {
	a.mu.Lock()
	stopped, resident := a.closed, a.companion
	var rawBotID string
	sourceNode := api.LocalNodeID
	if a.managed != nil {
		rawBotID, sourceNode = a.managed.snapshot.BotID, a.managed.nodeID
	}
	a.mu.Unlock()
	if rawBotID == "" && resident != nil {
		rawBotID = resident.State().ID
	}
	provider, ok := a.engine.(api.Provider)
	if stopped || rawBotID == "" || !ok {
		return workerwire.Pair{}, errors.New("primary native Bot identity is unavailable")
	}
	return workerwire.Pair{Target: target, BotID: api.ProfileBotID(rawBotID), SourceNode: sourceNode, SourceBackend: provider.ProviderInfo().ID}, nil
}

func (a *Application) newRegisteredWorkerNode(config backend.WorkerNodeConfig, source api.WorkSourceProvider) (workerNodeAdapter, error) {
	pair, err := a.workerSourcePair(configuredWorkerTarget(config))
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	lookup, requireLease := a.registeredWorkers, a.managed != nil
	a.mu.Unlock()
	if lookup == nil {
		return nil, errors.New("registered Worker has no trusted enrolled-node transport")
	}
	return &registeredWorkerAdapter{pair: pair, lookup: lookup, source: source, requireLease: requireLease}, nil
}

type registeredWorkerAdapter struct {
	mu           sync.Mutex
	pair         workerwire.Pair
	lookup       RegisteredWorkerAgentLookup
	source       api.WorkSourceProvider
	requireLease bool
	worker       *workerwire.Client
	closed       bool
}

func (w *registeredWorkerAdapter) open(ctx context.Context) (*workerwire.Client, error) {
	agent, err := w.lookup(ctx, w.pair)
	if err != nil || agent == nil {
		return nil, errors.New("enrolled Worker transport unavailable")
	}
	stream, err := agent.OpenWorkerStream(ctx, w.pair)
	if err != nil {
		return nil, err
	}
	client, err := workerwire.NewClient(ctx, w.pair, w.source, stream)
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	// This lookup is read-only, and never allocates or replays a task. Canonical
	// workspace authority comes from the paired native owner, not settings.
	workspace, err := client.ResolveWorkWorkspace(ctx, "task-"+strings.Repeat("0", 32), "")
	if err != nil || !path.IsAbs(workspace) || path.Clean(workspace) != workspace || path.Base(workspace) != "task-"+strings.Repeat("0", 32) || (w.requireLease && !client.LeaseAwareAdmission()) {
		client.Close()
		return nil, errors.New("enrolled Worker has no canonical workspace or native lease admission")
	}
	return client, nil
}

func (w *registeredWorkerAdapter) Probe(ctx context.Context) (backend.WorkerNodeFacts, error) {
	client, err := w.open(ctx)
	if err != nil {
		return backend.WorkerNodeFacts{}, err
	}
	client.Close()
	return backend.WorkerNodeFacts{}, nil
}
func (w *registeredWorkerAdapter) Connect(ctx context.Context) (api.WorkRuntime, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, errors.New("registered Worker observer closed")
	}
	if w.worker == nil {
		client, err := w.open(ctx)
		if err != nil {
			return nil, err
		}
		w.worker = client
	}
	return w.worker, nil
}
func (w *registeredWorkerAdapter) Close(context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.worker != nil {
		w.worker.Close()
		w.worker = nil
	}
	return nil
}
