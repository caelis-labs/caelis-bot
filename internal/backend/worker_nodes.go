package backend

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// WorkerNodeConfig is explicit connection setup, never model-visible discovery.
// Authentication remains in the system SSH client and private native adapter.
type WorkerNodeConfig struct {
	// Registered-agent routes are resolved only by native enrolled-node assembly.
	// They contain no SSH destination, executable, socket or workspace input.
	Transport     string `json:"transport,omitempty"`
	ID            string `json:"id"`
	Label         string `json:"label"`
	SSH           string `json:"ssh"`
	Helper        string `json:"helper"`
	Backend       string `json:"backend,omitempty"`
	Socket        string `json:"socket,omitempty"`
	Store         string `json:"store"`
	WorkspaceRoot string `json:"workspaceRoot"`
}

type WorkerNodeFacts struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Version string `json:"version"`
}

type WorkerNodeView struct {
	Config    WorkerNodeConfig `json:"config"`
	State     string           `json:"state"`
	Issue     string           `json:"issue"`
	Facts     WorkerNodeFacts  `json:"facts"`
	Connected bool             `json:"connected"`
}

type WorkerNodeSetup struct {
	Revision uint64           `json:"revision"`
	Nodes    []WorkerNodeView `json:"nodes"`
	Issue    string           `json:"issue"`
}

// WorkerNodeController has one exact machine/backend/role addressing contract.
type WorkerNodeController interface {
	Snapshot() WorkerNodeSetup
	Save(WorkerNodeConfig, uint64) (WorkerNodeSetup, error)
	ProbeTarget(context.Context, api.WorkTarget, uint64) (WorkerNodeSetup, error)
	ConnectTarget(context.Context, api.WorkTarget, uint64) (WorkerNodeSetup, error)
	DisconnectTarget(context.Context, api.WorkTarget, uint64) (WorkerNodeSetup, error)
}

func (s *Service) ProbeWorkerTarget(ctx context.Context, target api.WorkTarget, revision uint64) (WorkerNodeSetup, error) {
	controller, err := s.workerNodeController()
	if err != nil {
		return WorkerNodeSetup{}, err
	}
	return controller.ProbeTarget(ctx, target, revision)
}
func (s *Service) ConnectWorkerTarget(ctx context.Context, target api.WorkTarget, revision uint64) (WorkerNodeSetup, error) {
	controller, err := s.workerNodeController()
	if err != nil {
		return WorkerNodeSetup{}, err
	}
	return controller.ConnectTarget(ctx, target, revision)
}
func (s *Service) DisconnectWorkerTarget(ctx context.Context, target api.WorkTarget, revision uint64) (WorkerNodeSetup, error) {
	controller, err := s.workerNodeController()
	if err != nil {
		return WorkerNodeSetup{}, err
	}
	return controller.DisconnectTarget(ctx, target, revision)
}

func (s *Service) ConfigureWorkerNodes(controller WorkerNodeController) {
	s.mu.Lock()
	s.workerNodes = controller
	s.mu.Unlock()
}

func (s *Service) workerNodeController() (WorkerNodeController, error) {

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workerNodes == nil {
		return nil, errors.New("worker node setup is unavailable")
	}
	return s.workerNodes, nil
}

func (s *Service) WorkerNodes() WorkerNodeSetup {
	controller, err := s.workerNodeController()
	if err != nil {
		return WorkerNodeSetup{Nodes: []WorkerNodeView{}, Issue: "unavailable"}
	}
	return controller.Snapshot()
}

func (s *Service) SaveWorkerNode(config WorkerNodeConfig, revision uint64) (WorkerNodeSetup, error) {
	controller, err := s.workerNodeController()
	if err != nil {
		return WorkerNodeSetup{}, err
	}
	return controller.Save(config, revision)
}
