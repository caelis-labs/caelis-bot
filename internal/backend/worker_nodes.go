package backend

import (
	"context"
	"errors"
)

// WorkerNodeConfig is explicit connection setup, never model-visible discovery.
// Authentication remains in the system SSH client and private native adapter.
type WorkerNodeConfig struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	SSH           string `json:"ssh"`
	Helper        string `json:"helper"`
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

// WorkerNodeController lives in native assembly. Inspect never enrolls; Connect
// is the explicit action that establishes scoped ownership. Disconnect detaches.
type WorkerNodeController interface {
	Snapshot() WorkerNodeSetup
	Save(WorkerNodeConfig, uint64) (WorkerNodeSetup, error)
	Probe(context.Context, string, uint64) (WorkerNodeSetup, error)
	Connect(context.Context, string, uint64) (WorkerNodeSetup, error)
	Disconnect(context.Context, string, uint64) (WorkerNodeSetup, error)
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

func (s *Service) ProbeWorkerNode(ctx context.Context, id string, revision uint64) (WorkerNodeSetup, error) {
	controller, err := s.workerNodeController()
	if err != nil {
		return WorkerNodeSetup{}, err
	}
	return controller.Probe(ctx, id, revision)
}

func (s *Service) ConnectWorkerNode(ctx context.Context, id string, revision uint64) (WorkerNodeSetup, error) {
	controller, err := s.workerNodeController()
	if err != nil {
		return WorkerNodeSetup{}, err
	}
	return controller.Connect(ctx, id, revision)
}

func (s *Service) DisconnectWorkerNode(ctx context.Context, id string, revision uint64) (WorkerNodeSetup, error) {
	controller, err := s.workerNodeController()
	if err != nil {
		return WorkerNodeSetup{}, err
	}
	return controller.Disconnect(ctx, id, revision)
}
