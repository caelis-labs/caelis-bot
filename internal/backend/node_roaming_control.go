package backend

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const (
	NodeRoamingPrepareCoordinator = "prepare-coordinator"
	NodeRoamingPrepareNode        = "prepare-node"
	NodeRoamingConnectOutgoing    = "connect-outgoing"
	NodeRoamingStopSource         = "stop-source"
	NodeRoamingStartBot           = "start-bot"
)

type NodeRoamingPlanAction struct {
	NodeID string `json:"nodeId"`
	Label  string `json:"label"`
	Action string `json:"action"`
}
type NodeRoamingPlan struct {
	ID                   string                  `json:"id"`
	CoordinatorNodeID    string                  `json:"coordinatorNodeId"`
	Actions              []NodeRoamingPlanAction `json:"actions"`
	RequiresConfirmation bool                    `json:"requiresConfirmation"`
}
type NodeRoamingRequest struct {
	ReviewedPlanID           string `json:"reviewedPlanId"`
	AllowPersistentExecution bool   `json:"allowPersistentExecution"`
	ID                       string `json:"id"`
	ExpectedCatalogRevision  string `json:"expectedCatalogRevision"`
}
type NodeRoamingState struct {
	Available         bool   `json:"available"`
	OperationID       string `json:"operationId"`
	Outcome           string `json:"outcome"`
	Enabled           bool   `json:"enabled"`
	CoordinatorNodeID string `json:"coordinatorNodeId"`
	ActiveBotNodeID   string `json:"activeBotNodeId"`
	State             string `json:"state"`
	Reason            string `json:"reason"`
}
type NodeRoamingController interface {
	PrepareNodeRoaming(context.Context, NodeRoamingRequest) (NodeRoamingPlan, error)
	EnableNodeRoaming(context.Context, NodeRoamingRequest) (NodeRoamingState, error)
	DisableNodeRoaming(context.Context, NodeRoamingRequest) (NodeRoamingState, error)
	NodeRoamingState(context.Context) (NodeRoamingState, error)
}

func (m *nodeRoamingManagement) Close() error {
	var err error
	if c, ok := m.NodeRoamingController.(io.Closer); ok {
		err = c.Close()
	}
	if c, ok := m.NodeManagementController.(io.Closer); ok {
		err = errors.Join(err, c.Close())
	}
	return err
}

type nodeRoamingManagement struct {
	api.NodeManagementController
	NodeRoamingController
}

// ConfigureNodeRoaming preserves the existing settings facade and Wails
// service identity. Native APP assembly supplies the only lifecycle controller.
func (s *Service) ConfigureNodeRoaming(c NodeRoamingController) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c == nil || s.nodeManagement == nil {
		return errors.New("node roaming native assembly unavailable")
	}
	base := s.nodeManagement
	if old, ok := base.(*nodeRoamingManagement); ok {
		base = old.NodeManagementController
	}
	s.nodeManagement = &nodeRoamingManagement{base, c}
	return nil
}
func (s *Service) nodeRoamingController() (NodeRoamingController, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if facade, ok := s.nodeManagement.(*nodeRoamingManagement); ok {
		return facade.NodeRoamingController, nil
	}
	c, ok := s.nodeManagement.(NodeRoamingController)
	if !ok {
		return nil, errors.New("automatic roaming is unavailable")
	}
	return c, nil
}
func (s *Service) EnableNodeRoaming(ctx context.Context, r NodeRoamingRequest) (NodeRoamingState, error) {
	c, err := s.nodeRoamingController()
	if err != nil {
		return NodeRoamingState{}, err
	}
	return c.EnableNodeRoaming(ctx, r)
}
func (s *Service) DisableNodeRoaming(ctx context.Context, r NodeRoamingRequest) (NodeRoamingState, error) {
	c, err := s.nodeRoamingController()
	if err != nil {
		return NodeRoamingState{}, err
	}
	return c.DisableNodeRoaming(ctx, r)
}
func (s *Service) NodeRoamingState(ctx context.Context) (NodeRoamingState, error) {
	c, err := s.nodeRoamingController()
	if err != nil {
		return NodeRoamingState{State: "unavailable"}, nil
	}
	return c.NodeRoamingState(ctx)
}

// NodeRoamingEngine is installed once, before publishing Service. Only its
// delegate changes later; no renderer call can select or replace the engine.
type NodeRoamingEngine struct {
	mu     sync.RWMutex
	engine api.Engine
	swaps  uint64
}

func (s *Service) PrepareNodeRoamingEngine() (*NodeRoamingEngine, error) {
	if s.engine == nil {
		return nil, errors.New("source engine unavailable")
	}
	if _, ok := s.engine.(*NodeRoamingEngine); ok {
		return nil, errors.New("node roaming already attached")
	}
	p := &NodeRoamingEngine{engine: s.engine}
	s.engine = p
	return p, nil
}
func (p *NodeRoamingEngine) Current() api.Engine { p.mu.RLock(); defer p.mu.RUnlock(); return p.engine }
func (p *NodeRoamingEngine) Replace(e api.Engine) error {
	if e == nil {
		return errors.New("verified product engine required")
	}
	p.mu.Lock()
	p.engine = e
	p.swaps++
	p.mu.Unlock()
	return nil
}
func (p *NodeRoamingEngine) Connect(ctx context.Context) error { return p.Current().Connect(ctx) }
func (p *NodeRoamingEngine) Snapshot() api.Snapshot {
	p.mu.RLock()
	e, swaps := p.engine, p.swaps
	p.mu.RUnlock()
	v := e.Snapshot()
	v.Revision += swaps << 32
	return v
}
func (p *NodeRoamingEngine) Revision() uint64 {
	p.mu.RLock()
	e, swaps := p.engine, p.swaps
	p.mu.RUnlock()
	if source, ok := e.(api.RevisionSource); ok {
		return source.Revision() + (swaps << 32)
	}
	return p.Snapshot().Revision
}
func (p *NodeRoamingEngine) RecentSnapshot() api.Snapshot {
	p.mu.RLock()
	e, swaps := p.engine, p.swaps
	p.mu.RUnlock()
	if source, ok := e.(api.RecentSource); ok {
		v := source.RecentSnapshot()
		v.Revision += swaps << 32
		return v
	}
	return p.Snapshot()
}
func (p *NodeRoamingEngine) ComposerSnapshot() api.Snapshot {
	p.mu.RLock()
	e, swaps := p.engine, p.swaps
	p.mu.RUnlock()
	if source, ok := e.(api.ComposerSource); ok {
		v := source.ComposerSnapshot()
		v.Revision += swaps << 32
		return v
	}
	return p.Snapshot()
}
func (s *Service) RollbackNodeRoamingEngine(p *NodeRoamingEngine) {
	if s.engine == p {
		s.engine = p.Current()
	}
}

func (p *NodeRoamingEngine) Submit(ctx context.Context, in api.Submission, f []api.InputFile) (api.Receipt, error) {
	return p.Current().Submit(ctx, in, f)
}
func (p *NodeRoamingEngine) Interrupt(ctx context.Context) error { return p.Current().Interrupt(ctx) }
func (p *NodeRoamingEngine) Decide(ctx context.Context, d api.Decision) error {
	return p.Current().Decide(ctx, d)
}
func (p *NodeRoamingEngine) Close(ctx context.Context) error { return p.Current().Close(ctx) }

// ActivateNodeRoamingProduct changes product callbacks after the original
// lifecycle is stopped. Old durable source receipts and journals stay intact.
func (s *Service) ActivateNodeRoamingProduct(engine api.Engine) error {
	s.admission.Lock()
	defer s.admission.Unlock()
	return s.activateNodeRoamingProductLocked(engine)
}

func (s *Service) activateNodeRoamingProductLocked(engine api.Engine) error {
	p, ok := s.engine.(*NodeRoamingEngine)
	if !ok {
		return errors.New("stable roaming engine not installed")
	}
	if err := p.Replace(engine); err != nil {
		return err
	}
	s.mu.Lock()
	s.localGeneration = nil
	s.submitUser = nil
	s.botStatus = nil
	s.beforeInterrupt = nil
	if i, ok := engine.(api.BotInitializer); ok {
		s.initializer = i
	} else {
		s.initializer = nil
	}
	if m, ok := engine.(RemoteManagementController); ok {
		s.remoteManagement = m
	} else {
		s.remoteManagement = nil
	}
	s.mu.Unlock()
	s.restarting = false
	s.setupRequired = false
	return nil
}

func (s *Service) productDraftPort() (ProductDraftPort, bool) {
	e := s.engine
	if p, ok := e.(*NodeRoamingEngine); ok {
		e = p.Current()
	}
	d, ok := e.(ProductDraftPort)
	return d, ok
}

// ActivateNodeRoamingLocal adopts callbacks from an independently prepared local
// generation. The original engine is never restarted or given new authority.
func (s *Service) ActivateNodeRoamingLocal(other *Service) error {
	if other == nil || other == s {
		return errors.New("fresh local service required")
	}
	other.admission.RLock()
	admission, restarting, setupRequired := other.executionAdmission, other.restarting, other.setupRequired
	other.admission.RUnlock()
	other.mu.Lock()
	submit, status, before, init := other.submitUser, other.botStatus, other.beforeInterrupt, other.initializer
	setup, workers, interactions, management := other.setup, other.workerNodes, other.workInteractions, other.remoteManagement
	other.mu.Unlock()
	other.configurationMu.Lock()
	runtimeFile, runtimeSettings := other.runtimeFile, other.runtimeSettings
	executionFile, executionSettings := other.executionFile, other.executionSettings
	workFile, workSettings := other.workExecutionFile, other.workExecutionSettings
	providers := append([]api.ProviderInfo(nil), other.providers...)
	probe, manage, guard := other.probeRuntime, other.manageRuntime, other.switchGuard
	other.configurationMu.Unlock()

	// Admit the fresh engine only with its complete native generation. Host
	// callbacks, local presentation/draft/media, enrollment, roaming authority,
	// and the outer product-connection controller remain on the stable facade.
	s.admission.Lock()
	defer s.admission.Unlock()
	if err := s.activateNodeRoamingProductLocked(other.engine); err != nil {
		return err
	}
	s.mu.Lock()
	s.submitUser, s.botStatus, s.beforeInterrupt, s.initializer = submit, status, before, init
	s.setup, s.workerNodes, s.workInteractions, s.remoteManagement = setup, workers, interactions, management
	s.localGeneration = other
	s.mu.Unlock()
	s.executionAdmission, s.restarting, s.setupRequired = admission, restarting, setupRequired
	s.configurationMu.Lock()
	s.runtimeFile, s.runtimeSettings = runtimeFile, runtimeSettings
	s.executionFile, s.executionSettings = executionFile, executionSettings
	s.workExecutionFile, s.workExecutionSettings = workFile, workSettings
	s.providers, s.probeRuntime, s.manageRuntime, s.switchGuard = providers, probe, manage, guard
	s.configurationMu.Unlock()
	return nil
}

// NativeNodeRoamingController is a construction/lifecycle port, not a Wails method.
func NativeNodeRoamingController(s *Service) (NodeRoamingController, error) {
	return s.nodeRoamingController()
}

func (s *Service) PrepareNodeRoaming(ctx context.Context, r NodeRoamingRequest) (NodeRoamingPlan, error) {
	c, err := s.nodeRoamingController()
	if err != nil {
		return NodeRoamingPlan{}, err
	}
	return c.PrepareNodeRoaming(ctx, r)
}

// Optional capabilities belong to the current concrete engine, rather than the
// stable wrapper's method set. In particular unsupported local capabilities
// stay unsupported, and Caelis settings continue to come from its native owner.
func (s *Service) capabilityEngine() api.Engine {
	e := s.engine
	for {
		p, ok := e.(*NodeRoamingEngine)
		if !ok {
			return e
		}
		e = p.Current()
	}
}
func (s *Service) projectEngineRevision(revision uint64) uint64 {
	if p, ok := s.engine.(*NodeRoamingEngine); ok {
		p.mu.RLock()
		swaps := p.swaps
		p.mu.RUnlock()
		return revision + (swaps << 32)
	}
	return revision
}
func (s *Service) projectEngineSnapshot(v api.Snapshot) api.Snapshot {
	v.Revision = s.projectEngineRevision(v.Revision)
	return v
}

func HasNativeNodeManagement(s *Service) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nodeManagement != nil
}
func (m *nodeRoamingManagement) SetNodeCoordinator(ctx context.Context, r api.NodeCoordinatorSelection) (api.NodeCatalog, error) {
	state, err := m.NodeRoamingController.NodeRoamingState(ctx)
	if err != nil {
		return api.NodeCatalog{}, err
	}
	if state.Enabled || state.Outcome == "unknown" {
		if r.NodeID != state.CoordinatorNodeID || r.SourceRoutes != nil {
			return api.NodeCatalog{}, errors.New("disable automatic roaming before changing its coordinator")
		}
		catalog, err := m.NodeManagementController.NodeCatalog(ctx)
		if err != nil {
			return catalog, err
		}
		if r.ExpectedRevision != catalog.Revision {
			return api.NodeCatalog{}, errors.New("node catalog changed")
		}
		return catalog, nil
	}
	return m.NodeManagementController.SetNodeCoordinator(ctx, r)
}

// These native metadata checks never reconcile receipts or start recovery. They
// run before taking Service/configuration locks, so native control can safely
// publish ownership while configuration surfaces are open.
func (s *Service) blockLocalConfiguration() bool {
	controller, err := s.nodeRoamingController()
	if err != nil {
		return false
	}
	guard, ok := controller.(interface{ BlockLocalSetup() bool })
	return ok && guard.BlockLocalSetup()
}

func (s *Service) guardLocalConfiguration() error {
	if s.blockLocalConfiguration() {
		return errors.New("local configuration unavailable during automatic roaming")
	}
	return nil
}

func (s *Service) localGenerationService() *Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.localGeneration == s {
		return nil
	}
	return s.localGeneration
}
