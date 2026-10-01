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

// WorkerNodeController lives in native assembly. Inspect never enrolls; Connect
// is the explicit action that establishes scoped ownership. Disconnect detaches.
type WorkerNodeController interface {
	Snapshot() WorkerNodeSetup
	Save(WorkerNodeConfig, uint64) (WorkerNodeSetup, error)
	Probe(context.Context, string, uint64) (WorkerNodeSetup, error)
	Connect(context.Context, string, uint64) (WorkerNodeSetup, error)
	Disconnect(context.Context, string, uint64) (WorkerNodeSetup, error)
}

// WorkerNodeTargetController addresses one machine/backend/Worker capability.
// The legacy NodeID actions remain compatible only while that node is unambiguous.
type WorkerNodeTargetController interface {
	ProbeTarget(context.Context, api.WorkTarget, uint64) (WorkerNodeSetup, error)
	ConnectTarget(context.Context, api.WorkTarget, uint64) (WorkerNodeSetup, error)
	DisconnectTarget(context.Context, api.WorkTarget, uint64) (WorkerNodeSetup, error)
}

func (s *Service) workerTargetController() (WorkerNodeTargetController, error) {
	controller, err := s.workerNodeController()
	if err != nil {
		return nil, err
	}
	exact, ok := controller.(WorkerNodeTargetController)
	if !ok {
		return nil, errors.New("exact Worker target actions are unavailable")
	}
	return exact, nil
}
func (s *Service) ProbeWorkerTarget(ctx context.Context, target api.WorkTarget, revision uint64) (WorkerNodeSetup, error) {
	controller, err := s.workerTargetController()
	if err != nil {
		return WorkerNodeSetup{}, err
	}
	return controller.ProbeTarget(ctx, target, revision)
}
func (s *Service) ConnectWorkerTarget(ctx context.Context, target api.WorkTarget, revision uint64) (WorkerNodeSetup, error) {
	controller, err := s.workerTargetController()
	if err != nil {
		return WorkerNodeSetup{}, err
	}
	return controller.ConnectTarget(ctx, target, revision)
}
func (s *Service) DisconnectWorkerTarget(ctx context.Context, target api.WorkTarget, revision uint64) (WorkerNodeSetup, error) {
	controller, err := s.workerTargetController()
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
