package app

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/nodes"
)

// This constructor is called only by explicit native setup actions, never by
// Application.New or the default local resident/Worker path.
func (a *Application) newWorkerNodeAdapter(config backend.WorkerNodeConfig, directory string) (workerNodeAdapter, error) {
	if err := validateWorkerNode(config); err != nil {
		return nil, err
	}
	source, ok := a.engine.(api.WorkSourceProvider)
	if !ok {
		return nil, errors.New("resident driver cannot attest Worker dispatch")
	}
	if config.Transport == "registered-agent" {
		return a.newRegisteredWorkerNode(config, source)
	}
	if config.Backend == "codex" {
		return a.newCodexWorkerNode(config, source)
	}
	protocol := workerNodeProtocol(config)
	ssh, err := nodes.NewSSHWorker(nodes.SSHConfig{Protocol: protocol, Target: config.SSH, Helper: config.Helper, Store: config.Store, WorkspaceRoot: config.WorkspaceRoot})
	if err != nil {
		return nil, err
	}
	// Remote Worker model selection inherits its target Host default. The local
	// resident/work settings never silently select or configure a remote model.
	worker := caelis.NewWorker(caelis.WorkerOptions{Protocol: protocol, Target: workerTarget(config.ID), Directory: directory, Endpoint: ssh.Endpoint, Source: source, Workspace: ssh})
	return &sshWorkerNodeAdapter{ssh: ssh, worker: worker}, nil
}

// Empty backend preserves the protocol of existing private profiles. A newly
// configured explicit Caelis profile selects the bounded application scope.
func workerNodeProtocol(config backend.WorkerNodeConfig) caelis.WorkerProtocol {
	if config.Backend == "caelis" {
		return caelis.WorkerProtocolBoundedApplication
	}
	return caelis.WorkerProtocolSharedNative
}

type sshWorkerNodeAdapter struct {
	ssh    *nodes.SSHWorker
	worker *caelis.WorkerClient
}

func (a *sshWorkerNodeAdapter) Probe(ctx context.Context) (backend.WorkerNodeFacts, error) {
	facts, err := a.ssh.Probe(ctx)
	if err != nil {
		return backend.WorkerNodeFacts{}, err
	}
	return backend.WorkerNodeFacts{OS: facts.OS, Arch: facts.Arch}, nil
}

func (a *sshWorkerNodeAdapter) Connect(ctx context.Context) (api.WorkRuntime, error) {
	if err := a.worker.Connect(ctx); err != nil {
		return nil, err
	}
	return a.worker, nil
}

func (a *sshWorkerNodeAdapter) Close(ctx context.Context) error {
	return errors.Join(a.worker.Close(ctx), a.ssh.Close())
}
