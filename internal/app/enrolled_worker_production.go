package app

import (
	"context"
	"errors"
	"io"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

// Lookup is assembled for both ordinary APP and owned resident construction.
// It derives routes only from this owner's native enrollment, never wire paths.
func (a *Application) lookupEnrolledWorker(ctx context.Context, pair workerwire.Pair) (RegisteredWorkerAgent, error) {
	controller, err := backend.NativeNodeManagementController(a.Backend)
	if err != nil {
		return nil, err
	}
	management, ok := controller.(*nodeManagement)
	if !ok {
		return nil, errors.New("native enrollment unavailable")
	}
	n, ok := management.agent.(*nativeNodeManagement)
	if !ok {
		return nil, errors.New("native enrollment transport unavailable")
	}
	return n.lookupRegisteredWorker(ctx, pair)
}
func (n *nativeNodeManagement) lookupRegisteredWorker(ctx context.Context, pair workerwire.Pair) (RegisteredWorkerAgent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	actual, err := n.app.workerSourcePair(pair.Target)
	if err != nil || actual != pair || pair.Target.Backend != "codex" {
		return nil, errors.New("exact ordinary enrolled Worker source unavailable")
	}
	reg, err := n.registration(pair.Target.NodeID)
	if err != nil || reg.Join != api.NodeSSH || reg.HostHelperPath == "" {
		return nil, errors.New("update this enrolled node's helper before connecting Worker")
	}
	return enrolledSSHWorker{life: n.ownerCtx, registration: reg}, nil
}

type enrolledSSHWorker struct {
	life         context.Context
	registration NodeRegistration
}

func (w enrolledSSHWorker) OpenWorkerStream(ctx context.Context, pair workerwire.Pair) (io.ReadWriteCloser, error) {
	reg := w.registration
	if pair.Target.NodeID != reg.ID {
		return nil, errors.New("enrolled Worker target changed")
	}
	return nodeagent.OpenEnrolledWorkerStream(w.life, ctx, nodeagent.SSHConfig{Target: reg.SSHDestination}, reg.HostHelperPath, reg.Directory, reg.ID, pair)
}
func (n *nativeNodeManagement) SaveNodeRuntimeSettings(ctx context.Context, r api.NodeRuntimeSettingsRequest) (api.RuntimeCheck, error) {
	peer, err := n.agent(r.Guard.NodeID)
	if err != nil {
		return api.RuntimeCheck{}, err
	}
	port, ok := peer.(interface {
		SaveOwnedRuntimeSettings(context.Context, api.NodeRuntimeSettingsRequest) (api.RuntimeCheck, error)
	})
	if !ok {
		return api.RuntimeCheck{}, errors.New("update node support before saving machine Runtime path")
	}
	return port.SaveOwnedRuntimeSettings(ctx, r)
}
func (n *nativeNodeManagement) UpdateNodeHelper(ctx context.Context, r api.NodeHelperUpdateRequest) (api.NodeHelperUpdateResult, error) {
	n.controlMu.Lock()
	defer n.controlMu.Unlock()
	catalog, err := n.Catalog(ctx)
	if err != nil || catalog.Revision != r.ExpectedRevision {
		return api.NodeHelperUpdateResult{}, errors.New("node catalog changed before helper update")
	}
	reg, err := n.registration(r.NodeID)
	if err != nil || reg.Join != api.NodeSSH {
		return api.NodeHelperUpdateResult{}, errors.New("existing SSH enrollment required")
	}
	ssh := nodeagent.SSHConfig{Target: reg.SSHDestination}
	identity, err := nodeagent.ProbeSSHEnrollmentIdentity(ctx, ssh)
	if err != nil || identity.NodeID != reg.ID || identity.Directory != reg.Directory {
		return api.NodeHelperUpdateResult{}, errors.New("original native Node identity changed")
	}
	arch, err := nodeagent.ProbeArchitecture(ctx, ssh)
	if err != nil {
		return api.NodeHelperUpdateResult{}, err
	}
	artifact, err := n.options.Artifact(arch)
	if err != nil || artifact.HostPath == "" || nodeagent.VerifyArtifact(artifact) != nil {
		return api.NodeHelperUpdateResult{}, errors.New("verified helper pair required")
	}
	// Stop only this retained management observer, never any Runtime/Worker/Bot.
	n.mu.Lock()
	prior := n.clients[reg.ID]
	delete(n.clients, reg.ID)
	n.mu.Unlock()
	if closer, ok := prior.(io.Closer); ok {
		if err = closer.Close(); err != nil {
			return api.NodeHelperUpdateResult{}, err
		}
	}
	if err = nodeagent.InstallVerified(ctx, nodeagent.BootstrapPlan{SSH: ssh, Directory: reg.Directory, Artifact: artifact}); err != nil {
		return api.NodeHelperUpdateResult{}, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return api.NodeHelperUpdateResult{}, errors.New("native owner closed during helper update")
	}
	next := n.document
	next.Nodes = append([]NodeRegistration(nil), next.Nodes...)
	found := false
	for i, existing := range next.Nodes {
		if existing.ID == reg.ID && existing == reg {
			next.Nodes[i].HostHelperPath = filepath.Join(reg.Directory, "caelis-node")
			next.Nodes[i].HelperSourceRevision = artifact.SourceRevision
			found = true
		}
	}
	if !found {
		return api.NodeHelperUpdateResult{}, errors.New("original enrollment changed during helper update")
	}
	next.Revision++
	if err = writeNodeManagementDocument(filepath.Join(n.directory, "config.json"), next); err != nil {
		return api.NodeHelperUpdateResult{}, err
	}
	n.document = next
	return api.NodeHelperUpdateResult{NodeID: reg.ID, HelperSourceRevision: artifact.SourceRevision}, nil
}
