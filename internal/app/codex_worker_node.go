package app

import (
	"context"
	"errors"
	"path"
	"strings"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodes"
)

func (a *Application) newCodexWorkerNode(config backend.WorkerNodeConfig, source api.WorkSourceProvider) (workerNodeAdapter, error) {
	pair, err := a.workerSourcePair(configuredWorkerTarget(config))
	if err != nil {
		return nil, err
	}
	return &codexWorkerNodeAdapter{config: nodes.CodexSSHConfig{Destination: config.SSH, Helper: config.Helper, Socket: config.Socket, Pair: pair, Source: source}, root: config.WorkspaceRoot, connect: nodes.NewCodexSSHWorker}, nil
}

type codexWorkerNodeAdapter struct {
	mu      sync.Mutex
	config  nodes.CodexSSHConfig
	root    string
	connect func(context.Context, nodes.CodexSSHConfig) (*nodes.CodexSSHWorker, error)
	worker  *nodes.CodexSSHWorker
	closed  bool
}

func (a *codexWorkerNodeAdapter) open(ctx context.Context) (*nodes.CodexSSHWorker, error) {
	worker, err := a.connect(ctx, a.config)
	if err != nil {
		return nil, err
	}
	// Resolve is read-only: canonical target-local facts must match the selected
	// profile before its exact route can become ready. It never allocates a task.
	workspace, err := worker.ResolveWorkWorkspace(ctx, "task-"+strings.Repeat("0", 32), "")
	if err != nil || path.Dir(workspace) != a.root {
		worker.Close()
		return nil, errors.New("configured Codex Worker workspace root does not match native owner")
	}
	return worker, nil
}

func (a *codexWorkerNodeAdapter) Probe(ctx context.Context) (backend.WorkerNodeFacts, error) {
	worker, err := a.open(ctx)
	if err != nil {
		return backend.WorkerNodeFacts{}, err
	}
	worker.Close()
	return backend.WorkerNodeFacts{}, nil // Paired capability negotiation, no invented OS/version facts.
}

func (a *codexWorkerNodeAdapter) Connect(ctx context.Context) (api.WorkRuntime, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, errors.New("Codex Worker observer closed")
	}
	if a.worker != nil {
		return a.worker, nil
	}
	worker, err := a.open(ctx)
	if err != nil {
		return nil, err
	}
	a.worker = worker
	return worker, nil
}
func (a *codexWorkerNodeAdapter) Close(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	if a.worker != nil {
		a.worker.Close()
		a.worker = nil
	}
	return nil
}
