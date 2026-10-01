package app

import (
	"context"
	"errors"
	"path/filepath"

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
		if config.Backend == "caelis" && a.managed == nil {
			return a.newEnrolledCaelisWorker(config, directory, source)
		}
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

// Caelis keeps its existing target-native bootstrap/Host/application adapter.
// Enrollment supplies all machine paths; model/renderer input cannot select them.
func (a *Application) newEnrolledCaelisWorker(config backend.WorkerNodeConfig, directory string, source api.WorkSourceProvider) (workerNodeAdapter, error) {
	pair, err := a.workerSourcePair(configuredWorkerTarget(config))
	if err != nil {
		return nil, err
	}
	if pair.Target.NodeID == pair.SourceNode && pair.Target.Backend == pair.SourceBackend {
		return nil, errors.New("direct native Worker remains the default")
	}
	controller, err := backend.NativeNodeManagementController(a.Backend)
	if err != nil {
		return nil, err
	}
	management, ok := controller.(*nodeManagement)
	if !ok {
		return nil, errors.New("enrolled Caelis Worker unavailable")
	}
	native, ok := management.agent.(*nativeNodeManagement)
	if !ok {
		return nil, errors.New("enrolled Caelis transport unavailable")
	}
	reg, err := native.registration(config.ID)
	if err != nil || reg.Join != api.NodeSSH || reg.HostHelperPath == "" {
		return nil, errors.New("update enrolled Caelis node support before connecting")
	}
	settings, err := (&roamingNativeAssembly{app: a}).runtimeSettings(context.Background(), reg, api.NodeCaelis)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(reg.Directory, "caelis-worker-tasks")
	ssh, err := nodes.NewSSHWorker(nodes.SSHConfig{Protocol: caelis.WorkerProtocolBoundedApplication, Target: reg.SSHDestination, Helper: reg.HostHelperPath, HelperArgs: []string{"worker-bootstrap"}, Store: settings.CaelisStore, WorkspaceRoot: root})
	if err != nil {
		return nil, err
	}
	worker := caelis.NewWorker(caelis.WorkerOptions{Protocol: caelis.WorkerProtocolBoundedApplication, Target: pair.Target, Directory: directory, Endpoint: ssh.Endpoint, Source: source, Workspace: ssh})
	return &sshWorkerNodeAdapter{ssh: ssh, worker: worker}, nil
}
