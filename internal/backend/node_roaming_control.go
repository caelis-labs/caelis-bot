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
func (p *NodeRoamingEngine) Revision() uint64               { return p.Snapshot().Revision }
func (p *NodeRoamingEngine) RecentSnapshot() api.Snapshot   { return p.Snapshot() }
func (p *NodeRoamingEngine) ComposerSnapshot() api.Snapshot { return p.Snapshot() }
func (p *NodeRoamingEngine) Submit(ctx context.Context, in api.Submission, f []api.InputFile) (api.Receipt, error) {
	return p.Current().Submit(ctx, in, f)
}
func (p *NodeRoamingEngine) Interrupt(ctx context.Context) error { return p.Current().Interrupt(ctx) }
func (p *NodeRoamingEngine) Decide(ctx context.Context, d api.Decision) error {
	return p.Current().Decide(ctx, d)
}
func (p *NodeRoamingEngine) Close(ctx context.Context) error { return p.Current().Close(ctx) }
func (p *NodeRoamingEngine) ProviderInfo() api.ProviderInfo {
	if e, ok := p.Current().(api.Provider); ok {
		return e.ProviderInfo()
	}
	return api.ProviderInfo{}
}
func (p *NodeRoamingEngine) LoadEarlier(ctx context.Context) error {
	if e, ok := p.Current().(api.HistorySource); ok {
		return e.LoadEarlier(ctx)
	}
	return errors.New("history unavailable")
}
func (p *NodeRoamingEngine) ImageInput(ctx context.Context) (api.ImageInputCapability, error) {
	if e, ok := p.Current().(api.ImageInputProvider); ok {
		return e.ImageInput(ctx)
	}
	return api.ImageInputCapability{State: "unknown"}, nil
}
func (p *NodeRoamingEngine) Artifact(id string) (string, error) {
	if e, ok := p.Current().(api.ArtifactResolver); ok {
		return e.Artifact(id)
	}
	return "", errors.New("artifact unavailable")
}
func (p *NodeRoamingEngine) ApprovalURL(id string) (string, error) {
	if e, ok := p.Current().(api.ApprovalNavigator); ok {
		return e.ApprovalURL(id)
	}
	return "", errors.New("approval navigation unavailable")
}
func (p *NodeRoamingEngine) Login(ctx context.Context) (string, error) {
	if e, ok := p.Current().(api.Authenticator); ok {
		return e.Login(ctx)
	}
	return "", errors.New("login unavailable")
}
func (p *NodeRoamingEngine) CancelLogin(ctx context.Context) error {
	if e, ok := p.Current().(api.Authenticator); ok {
		return e.CancelLogin(ctx)
	}
	return nil
}
func (p *NodeRoamingEngine) ExecutionOptions() api.ExecutionOptions {
	if e, ok := p.Current().(api.ExecutionProvider); ok {
		return e.ExecutionOptions()
	}
	return api.ExecutionOptions{}
}
func (p *NodeRoamingEngine) Models(ctx context.Context) ([]api.ModelOption, error) {
	if e, ok := p.Current().(api.ExecutionProvider); ok {
		return e.Models(ctx)
	}
	return nil, errors.New("models unavailable")
}
func (p *NodeRoamingEngine) ChangeExecution(ctx context.Context, v api.ExecutionSettings, save func() error) error {
	if e, ok := p.Current().(api.ExecutionProvider); ok {
		return e.ChangeExecution(ctx, v, save)
	}
	return errors.New("execution configuration unavailable")
}
func (p *NodeRoamingEngine) ChangeWorkExecution(ctx context.Context, v api.WorkExecutionSettings, save func() error) error {
	if e, ok := p.Current().(api.WorkExecutionProvider); ok {
		return e.ChangeWorkExecution(ctx, v, save)
	}
	return errors.New("worker configuration unavailable")
}
func (p *NodeRoamingEngine) ChangeRuntime(ctx context.Context, v api.RuntimeSettings, save func() error) (api.RuntimeCheck, error) {
	if e, ok := p.Current().(api.RuntimeConfigurator); ok {
		return e.ChangeRuntime(ctx, v, save)
	}
	return api.RuntimeCheck{}, errors.New("runtime change unavailable")
}
func (p *NodeRoamingEngine) InterruptTurn(ctx context.Context, turn string, before func()) error {
	if e, ok := p.Current().(exactTurnInterrupter); ok {
		return e.InterruptTurn(ctx, turn, before)
	}
	return errors.New("exact turn interruption unavailable")
}
func (p *NodeRoamingEngine) AcknowledgePresentation(ctx context.Context, v api.Snapshot) error {
	if e, ok := p.Current().(api.PresentationAcknowledger); ok {
		return e.AcknowledgePresentation(ctx, v)
	}
	return nil
}

// ActivateNodeRoamingProduct changes product callbacks after the original
// lifecycle is stopped. Old durable source receipts and journals stay intact.
func (s *Service) ActivateNodeRoamingProduct(engine api.Engine) error {
	p, ok := s.engine.(*NodeRoamingEngine)
	if !ok {
		return errors.New("stable roaming engine not installed")
	}
	s.admission.Lock()
	defer s.admission.Unlock()
	if err := p.Replace(engine); err != nil {
		return err
	}
	s.mu.Lock()
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
	if err := s.ActivateNodeRoamingProduct(other.engine); err != nil {
		return err
	}
	other.mu.Lock()
	submit, status, before, init := other.submitUser, other.botStatus, other.beforeInterrupt, other.initializer
	connection := other.productConnection
	other.mu.Unlock()
	s.mu.Lock()
	s.submitUser = submit
	s.botStatus = status
	s.beforeInterrupt = before
	s.initializer = init
	s.productConnection = connection
	s.mu.Unlock()
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
