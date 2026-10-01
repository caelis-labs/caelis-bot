package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

// Stage input is native-only. Registrations come from the enrolled document,
// and Source is the original concrete APP's immutable stopped-owner proof.

type NodeRoamingStageInput struct {
	ReviewedPlanID           string
	AllowPersistentExecution bool
	BotID                    string
	OperationID              string
	Coordinator              NodeRegistration
	Nodes                    []NodeRegistration
	SourceTarget             api.WorkTarget
	Source                   nodeplane.RuntimeProofPort
	Snapshot                 nodeplane.SnapshotRef
	Payload                  []byte
	Resume                   bool
}
type NodeRoamingStage struct {
	Broker nodeplane.ActiveLeaseReader
	// Disable must freeze all claimants, require exact owner safe idle, release
	// conservatively, and retain latest Notebook until RestoreLocal installs it.
	Disable func(context.Context, string) error
	Close   func() error
}
type NodeRoamingProductLocation struct {
	ClientFactory productClientFactory
	Pairing       backend.ProductPairing
	Generation    string
}
type NodeRoamingRecoveryInput struct {
	StageInput                                                                    NodeRoamingStageInput
	OperationID, StageOperationID, OperationKind, Phase, LocalGenerationDirectory string
	SourceRetiredIntent                                                           bool
}
type NodeRoamingRecovery struct {
	OperationID, Outcome, Phase string
	Stage                       NodeRoamingStage
	Local                       *Application
}
type NodeRoamingOptions struct {
	Recover     func(context.Context, NodeRoamingRecoveryInput) (NodeRoamingRecovery, error)
	PreparePlan func(context.Context, NodeRoamingStageInput) (backend.NodeRoamingPlan, error)
	// Preflight validates/stages native artifacts without stopping the source.
	Preflight      func(context.Context, NodeRoamingStageInput) error
	Stage          func(context.Context, NodeRoamingStageInput) (NodeRoamingStage, error)
	ResolveProduct func(context.Context, nodeplane.Lease, NodeRegistration) (NodeRoamingProductLocation, error)
	// RestoreLocal builds a fresh complete local APP from the latest stopped
	// Notebook. It must not reopen the source APP or import its native receipts.
	RestoreLocal          func(context.Context, NodeRoamingStage) (*Application, error)
	RefreshNodeManagement *NodeManagementNativeOptions
}
type nodeRoamingDocument struct {
	SourceBackend            string `json:"sourceBackend,omitempty"`
	Phase                    string `json:"phase"`
	OperationKind            string `json:"operationKind"`
	StageOperationID         string `json:"stageOperationId"`
	SourceRetiredIntent      bool   `json:"sourceRetiredIntent"`
	LocalGenerationDirectory string `json:"localGenerationDirectory,omitempty"`
	ReviewedPlanID           string `json:"reviewedPlanId"`
	AllowPersistentExecution bool   `json:"allowPersistentExecution"`
	Version                  int    `json:"version"`
	Enabled                  bool   `json:"enabled"`
	CoordinatorNodeID        string `json:"coordinatorNodeId"`
	BotID                    string `json:"botId"`
	OperationID              string `json:"operationId"`
	Outcome                  string `json:"outcome"`
}
type nodeRoamingControl struct {
	op                  sync.Mutex
	mu                  sync.Mutex
	app                 *Application
	options             NodeRoamingOptions
	proxy               *backend.NodeRoamingEngine
	doc                 nodeRoamingDocument
	state               backend.NodeRoamingState
	stage               NodeRoamingStage
	registrations       map[string]NodeRegistration
	product             *productEngine
	lease               nodeplane.Lease
	location            NodeRoamingProductLocation
	local               *Application
	pendingLocalRestore bool
	ctx                 context.Context
	cancel              context.CancelFunc
	workers             sync.WaitGroup
	closed              bool
	prepareSource       func(context.Context, string) ([]byte, nodeplane.SnapshotRef, nodeplane.RuntimeProofPort, error)
	clientFactory       productClientFactory
	startLocal          func(context.Context, *Application) error
	prepareLocalSource  func(context.Context, *Application, string) ([]byte, nodeplane.SnapshotRef, nodeplane.RuntimeProofPort, error)
}

// AttachNodeRoaming is called before APP Start or Wails service publication.
// It starts no broker, runtime, SSH process or model unless explicit persisted
// enablement is restored by RestoreNodeRoaming.
func AttachNodeRoaming(a *Application, o NodeRoamingOptions) error {
	if a == nil || a.Backend == nil {
		return errors.New("APP service unavailable")
	}
	a.mu.Lock()
	started := a.started
	a.mu.Unlock()
	if started {
		return errors.New("attach roaming before APP start")
	}
	doc, err := loadNodeRoamingDocument(filepath.Join(a.root, "nodeplane", "roaming.json"))
	if err != nil {
		return err
	}
	// Verify facade availability before installing the one-time stable wrapper.
	if !backend.HasNativeNodeManagement(a.Backend) {
		return errors.New("native node management unavailable")
	}
	proxy, err := a.Backend.PrepareNodeRoamingEngine()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &nodeRoamingControl{app: a, options: o, proxy: proxy, ctx: ctx, cancel: cancel, prepareSource: a.PrepareRoamingBootstrap, clientFactory: newSSHProductClient, doc: doc, registrations: map[string]NodeRegistration{}, pendingLocalRestore: doc.LocalGenerationDirectory != "", startLocal: func(_ context.Context, a *Application) error { return a.Start() }, prepareLocalSource: func(ctx context.Context, a *Application, id string) ([]byte, nodeplane.SnapshotRef, nodeplane.RuntimeProofPort, error) {
		return a.PrepareRoamingBootstrap(ctx, id)
	}}
	c.state = backend.NodeRoamingState{Available: o.PreparePlan != nil && o.Preflight != nil && o.Stage != nil && o.ResolveProduct != nil && o.RestoreLocal != nil, State: "disabled", Enabled: doc.Enabled, CoordinatorNodeID: doc.CoordinatorNodeID, OperationID: doc.OperationID, Outcome: doc.Outcome}
	if doc.Enabled {
		c.state.State = "waiting"
	}
	if doc.Outcome == "unknown" {
		c.state.State = "unknown"
	}
	if err := a.Backend.ConfigureNodeRoaming(c); err != nil {
		cancel()
		a.Backend.RollbackNodeRoamingEngine(proxy)
		return err
	}
	if doc.Enabled || doc.Outcome == "unknown" || doc.LocalGenerationDirectory != "" {
		if err := a.Backend.ActivateNodeRoamingProduct(roamingWaitingEngine{}); err != nil {
			return err
		}
	}
	return nil
}
func loadNodeRoamingDocument(filename string) (nodeRoamingDocument, error) {
	doc := nodeRoamingDocument{Version: 1, Phase: "local"}
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return doc, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16<<10 {
		return doc, errors.New("private roaming intent unavailable")
	}
	b, err := os.ReadFile(filename)
	if err != nil {
		return doc, err
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&doc) != nil {
		return doc, errors.New("invalid roaming intent")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || doc.Version != 1 {
		return doc, errors.New("invalid roaming intent")
	}
	switch doc.Phase {
	case "local", "preparing", "retiring-source", "staging", "active", "quiescing", "restoring-local":
	default:
		return doc, errors.New("invalid roaming phase")
	}
	if doc.OperationID != "" && !productIdentifier.MatchString(doc.OperationID) {
		return doc, errors.New("invalid original roaming operation")
	}
	if doc.OperationKind != "" && doc.OperationKind != "enable" && doc.OperationKind != "disable" {
		return doc, errors.New("invalid roaming operation kind")
	}
	if doc.Outcome != "" && doc.Outcome != "accepted" && doc.Outcome != "rejected" && doc.Outcome != "unknown" {
		return doc, errors.New("invalid roaming outcome")
	}
	if doc.Enabled || doc.Outcome == "unknown" {
		if doc.OperationID == "" || doc.OperationKind == "" || doc.CoordinatorNodeID == "" || doc.ReviewedPlanID == "" || !doc.AllowPersistentExecution {
			return doc, errors.New("incomplete original roaming intent")
		}
	}
	if doc.Phase == "active" && (!doc.Enabled || doc.StageOperationID == "" || doc.BotID == "" || !doc.SourceRetiredIntent) {
		return doc, errors.New("incomplete active roaming authority")
	}
	if doc.StageOperationID != "" && !productIdentifier.MatchString(doc.StageOperationID) {
		return doc, errors.New("invalid original stage operation")
	}
	switch doc.Phase {
	case "preparing":
		if doc.Enabled || doc.OperationKind != "enable" || doc.Outcome != "unknown" || doc.SourceRetiredIntent || doc.StageOperationID == "" {
			return doc, errors.New("inconsistent native preparation intent")
		}
	case "retiring-source", "staging":
		if doc.Enabled || doc.OperationKind != "enable" || doc.Outcome != "unknown" || !doc.SourceRetiredIntent || doc.StageOperationID == "" || (doc.Phase == "staging" && doc.BotID == "") {
			return doc, errors.New("inconsistent source retirement intent")
		}
	case "quiescing", "restoring-local":
		if !doc.Enabled || doc.OperationKind != "disable" || doc.Outcome != "unknown" || !doc.SourceRetiredIntent || doc.StageOperationID == "" || doc.BotID == "" {
			return doc, errors.New("inconsistent native disable intent")
		}
	case "local":
		if doc.Enabled || (doc.SourceRetiredIntent && doc.LocalGenerationDirectory == "") {
			return doc, errors.New("missing restored native generation")
		}
	}
	if doc.SourceBackend != "" && doc.SourceBackend != "codex" && doc.SourceBackend != "caelis" {
		return doc, errors.New("invalid original source backend")
	}
	if doc.LocalGenerationDirectory != "" && (!filepath.IsAbs(doc.LocalGenerationDirectory) || filepath.Clean(doc.LocalGenerationDirectory) != doc.LocalGenerationDirectory) {
		return doc, errors.New("invalid restored local generation")
	}
	return doc, nil
}
func (c *nodeRoamingControl) filename() string {
	return filepath.Join(c.app.root, "nodeplane", "roaming.json")
}

// RestoreNodeRoaming is invoked by native startup before original APP Start.
// Recovery and resume only reconnect to confirmed native ownership; they never launch hosts.
func RestoreNodeRoaming(ctx context.Context, a *Application) (bool, error) {
	port, err := backend.NativeNodeRoamingController(a.Backend)
	if err != nil {
		return false, err
	}
	c, ok := port.(*nodeRoamingControl)
	if !ok {
		return false, errors.New("native roaming controller mismatch")
	}
	c.op.Lock()
	defer c.op.Unlock()
	if c.doc.Outcome == "unknown" || c.doc.Phase == "quiescing" || c.doc.Phase == "restoring-local" || c.doc.LocalGenerationDirectory != "" {
		if err := c.recoverOriginal(ctx); err != nil {
			return true, err
		}
		if c.doc.Outcome == "unknown" {
			return true, errors.New("saved original roaming operation requires native reconciliation")
		}
		if c.local != nil {
			return true, nil
		}
	}
	if !c.doc.Enabled {
		return false, nil
	}
	if c.doc.Phase != "active" || !c.state.Available || c.doc.BotID == "" {
		return true, errors.New("saved native roaming phase unavailable")
	}
	input, err := c.recoveryInput()
	if err != nil {
		return true, err
	}
	input.StageInput.Resume = true
	stage, err := c.options.Stage(ctx, input.StageInput)
	if err != nil || !validRoamingStage(stage) {
		c.setState("unknown", "native-resume-unconfirmed")
		return true, errors.New("saved native roaming could not resume")
	}
	c.stage = stage
	for _, reg := range input.StageInput.Nodes {
		c.registrations[reg.ID] = reg
	}
	if c.options.RefreshNodeManagement != nil {
		if err := AttachNodeManagement(a, *c.options.RefreshNodeManagement); err != nil {
			return true, err
		}
		if err := a.Backend.ConfigureNodeRoaming(c); err != nil {
			return true, err
		}
	}
	if err := a.Backend.ActivateNodeRoamingProduct(roamingWaitingEngine{}); err != nil {
		return true, err
	}
	a.Backend.ConfigureProductConnection(c)
	c.startObserver()
	_ = c.followOwner(ctx)
	return true, nil
}
func (c *nodeRoamingControl) save() error { return localstate.Write(c.filename(), c.doc) }
func (c *nodeRoamingControl) currentState(ctx context.Context) (backend.NodeRoamingState, error) {
	if err := ctx.Err(); err != nil {
		return backend.NodeRoamingState{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state, nil
}
func (c *nodeRoamingControl) setState(state, reason string) {
	if state == "unknown" {
		c.doc.Outcome = "unknown"
	}
	c.mu.Lock()
	c.state.State, c.state.Reason = state, reason
	c.mu.Unlock()
}
func (c *nodeRoamingControl) input(ctx context.Context, r backend.NodeRoamingRequest) (NodeRoamingStageInput, error) {
	if !productIdentifier.MatchString(r.ID) || r.ExpectedCatalogRevision == "" {
		return NodeRoamingStageInput{}, errors.New("original operation ID and catalog revision required")
	}
	catalog, err := c.app.Backend.NodeCatalog(ctx)
	if err != nil {
		return NodeRoamingStageInput{}, err
	}
	if catalog.Revision != r.ExpectedCatalogRevision || catalog.Broker == nil || catalog.Broker.NodeID == "" {
		return NodeRoamingStageInput{}, errors.New("designated coordinator changed; refresh before enabling")
	}
	doc, err := loadNodeManagementDocument(filepath.Join(c.app.root, "nodeplane", "config.json"))
	if err != nil {
		return NodeRoamingStageInput{}, err
	}
	if doc.Coordinator != catalog.Broker.NodeID {
		return NodeRoamingStageInput{}, errors.New("exact coordinator enrollment changed")
	}
	regs := append([]NodeRegistration{{ID: api.LocalNodeID, Label: "This machine", Join: api.NodeLocal}}, doc.Nodes...)
	var coordinator NodeRegistration
	for _, reg := range regs {
		if reg.ID == doc.Coordinator {
			coordinator = reg
		}
	}
	if coordinator.ID == "" {
		return NodeRoamingStageInput{}, errors.New("coordinator is not enrolled")
	}
	source := ActiveNodeRoamingApplication(c.app)
	nativeProvider, ok := source.engine.(api.Provider)
	if !ok {
		return NodeRoamingStageInput{}, errors.New("local source provider unavailable")
	}
	provider := nativeProvider.ProviderInfo().ID
	if provider != "codex" && provider != "caelis" {
		return NodeRoamingStageInput{}, errors.New("local source backend does not support native roaming")
	}
	return NodeRoamingStageInput{ReviewedPlanID: r.ReviewedPlanID, AllowPersistentExecution: r.AllowPersistentExecution, OperationID: r.ID, Coordinator: coordinator, Nodes: regs, SourceTarget: api.WorkTarget{NodeID: api.LocalNodeID, Backend: provider, Role: api.RoleBot}}, nil
}
func (c *nodeRoamingControl) EnableNodeRoaming(ctx context.Context, r backend.NodeRoamingRequest) (backend.NodeRoamingState, error) {
	c.op.Lock()
	defer c.op.Unlock()
	state, _ := c.currentState(ctx)
	if c.closed || !state.Available {
		return state, errors.New("native roaming assembly unavailable")
	}
	if state.Outcome == "unknown" {
		return state, errors.New("original roaming operation requires native reconciliation")
	}
	if state.Enabled {
		return state, nil
	}
	input, err := c.input(ctx, r)
	if err != nil {
		return state, err
	}
	plan, err := c.options.PreparePlan(ctx, input)
	if err != nil {
		return state, err
	}
	if plan.ID == "" || plan.CoordinatorNodeID != input.Coordinator.ID || plan.ID != r.ReviewedPlanID || !r.AllowPersistentExecution {
		return state, errors.New("review the native deployment plan before enabling")
	}
	c.doc = nodeRoamingDocument{SourceBackend: input.SourceTarget.Backend, LocalGenerationDirectory: c.doc.LocalGenerationDirectory, Phase: "preparing", OperationKind: "enable", StageOperationID: r.ID, ReviewedPlanID: r.ReviewedPlanID, AllowPersistentExecution: r.AllowPersistentExecution, Version: 1, CoordinatorNodeID: input.Coordinator.ID, OperationID: r.ID, Outcome: "unknown"}
	if err := c.save(); err != nil {
		return state, err
	}
	c.mu.Lock()
	c.state.CoordinatorNodeID = input.Coordinator.ID
	c.state.OperationID = r.ID
	c.state.Outcome = "unknown"
	c.state.State = "enabling"
	c.mu.Unlock()
	if err := c.options.Preflight(ctx, input); err != nil {
		if errors.Is(err, ErrNodeRoamingPreflight) {
			c.doc.Outcome = "rejected"
			c.doc.Phase = "local"
			if saveErr := c.save(); saveErr != nil {
				c.setState("unknown", "intent-write-unconfirmed")
				return c.currentState(ctx)
			}
			c.mu.Lock()
			c.state.Outcome = "rejected"
			c.state.State = "disabled"
			c.state.Reason = "native-preparation-refused"
			c.mu.Unlock()
		} else {
			c.setState("unknown", "native-preparation-unconfirmed")
		}
		result, _ := c.currentState(context.Background())
		return result, err
	}
	c.doc.Phase = "retiring-source"
	c.doc.SourceRetiredIntent = true
	if err := c.save(); err != nil {
		c.setState("unknown", "intent-write-unconfirmed")
		return c.currentState(ctx)
	}
	source := c.prepareSource
	c.mu.Lock()
	freshSource := c.local
	c.mu.Unlock()
	if freshSource != nil {
		source = func(ctx context.Context, id string) ([]byte, nodeplane.SnapshotRef, nodeplane.RuntimeProofPort, error) {
			return c.prepareLocalSource(ctx, freshSource, id)
		}
	}
	input.Payload, input.Snapshot, input.Source, err = source(ctx, input.SourceTarget.NodeID)
	if err != nil {
		if errors.Is(err, ErrNodeRoamingPreflight) {
			c.doc.SourceRetiredIntent = false
			c.doc.Phase = "local"
			c.doc.Outcome = "rejected"
			if saveErr := c.save(); saveErr != nil {
				c.setState("unknown", "intent-write-unconfirmed")
				return c.currentState(ctx)
			}
			c.mu.Lock()
			c.state.Outcome = "rejected"
			c.state.State = "disabled"
			c.state.Reason = "source-not-eligible"
			c.mu.Unlock()
		} else {
			_ = c.withdrawObserver("source-retirement-unconfirmed")
			c.setState("unknown", "source-retirement-unconfirmed")
		}
		return c.currentState(ctx)
	}
	if freshSource != nil {
		_ = freshSource.Close()
		c.mu.Lock()
		c.local = nil
		c.mu.Unlock()
	}
	_ = c.withdrawObserver("native-stage-preparing")
	c.doc.Phase = "staging"
	c.doc.BotID = input.Snapshot.BotID
	input.BotID = c.doc.BotID
	if err := c.save(); err != nil {
		c.setState("unknown", "intent-write-unconfirmed")
		return c.currentState(ctx)
	}
	stage, err := c.options.Stage(ctx, input)
	if err != nil || stage.Broker == nil || stage.Disable == nil || stage.Close == nil {
		c.setState("unknown", "native-stage-unconfirmed")
		return c.currentState(ctx)
	}
	c.stage = stage
	for _, reg := range input.Nodes {
		c.registrations[reg.ID] = reg
	}
	if c.options.RefreshNodeManagement != nil {
		if err := AttachNodeManagement(c.app, *c.options.RefreshNodeManagement); err != nil {
			c.setState("unknown", "native-catalog-unavailable")
			return c.currentState(ctx)
		}
		if err := c.app.Backend.ConfigureNodeRoaming(c); err != nil {
			return state, err
		}
	}
	c.doc.Enabled = true
	c.doc.Phase = "active"
	c.doc.Outcome = "accepted"
	if err := c.save(); err != nil {
		c.setState("unknown", "intent-write-unconfirmed")
		return c.currentState(ctx)
	}
	c.mu.Lock()
	c.state.Enabled = true
	c.state.Outcome = "accepted"
	c.state.State = "waiting"
	c.mu.Unlock()
	if err := c.app.Backend.ActivateNodeRoamingProduct(roamingWaitingEngine{}); err != nil {
		return state, err
	}
	c.app.Backend.ConfigureProductConnection(c)
	c.startObserver()
	_ = c.followOwner(ctx)
	return c.currentState(ctx)
}
func (c *nodeRoamingControl) DisableNodeRoaming(ctx context.Context, r backend.NodeRoamingRequest) (backend.NodeRoamingState, error) {
	c.op.Lock()
	defer c.op.Unlock()
	state, _ := c.currentState(ctx)
	if !state.Enabled {
		return state, nil
	}
	if state.Outcome == "unknown" {
		return state, errors.New("original roaming operation requires native reconciliation")
	}
	if _, err := c.input(ctx, r); err != nil {
		return state, err
	}
	if c.stage.Disable == nil {
		return state, errors.New("safe native disable unavailable")
	}
	c.doc.OperationID = r.ID
	c.doc.OperationKind = "disable"
	c.doc.Phase = "quiescing"
	c.doc.Outcome = "unknown"
	c.mu.Lock()
	c.state.OperationID = r.ID
	c.state.Outcome = "unknown"
	c.state.State = "disabling"
	c.mu.Unlock()
	if err := c.save(); err != nil {
		c.setState("unknown", "intent-write-unconfirmed")
		return c.currentState(ctx)
	}
	if err := c.stage.Disable(ctx, r.ID); err != nil {
		if errors.Is(err, ErrNodeRoamingBusy) {
			c.doc.Outcome = "rejected"
			c.doc.Phase = "active"
			if saveErr := c.save(); saveErr != nil {
				c.setState("unknown", "intent-write-unconfirmed")
				return c.currentState(ctx)
			}
			c.mu.Lock()
			c.state.State = state.State
			c.state.Reason = "owner-not-safe-idle"
			c.state.Outcome = "rejected"
			c.mu.Unlock()
		} else {
			c.setState("unknown", "native-disable-unconfirmed")
		}
		return c.currentState(ctx)
	}
	c.doc.Phase = "restoring-local"
	if err := c.save(); err != nil {
		c.setState("unknown", "intent-write-unconfirmed")
		return c.currentState(ctx)
	}
	// Disable has fenced old owners; keep the APP facade cold while preparing
	// a fresh local candidate. Observation cannot reactivate claimants here.
	_ = c.withdrawObserver("local-generation-preparing")
	local, err := c.options.RestoreLocal(ctx, c.stage)
	if err != nil || local == nil || local == c.app || local.Backend == nil {
		c.setState("unknown", "local-restore-unconfirmed")
		return c.currentState(ctx)
	}
	local.mu.Lock()
	started := local.started
	local.mu.Unlock()
	if started {
		_ = local.Close()
		c.setState("unknown", "local-candidate-already-active")
		return c.currentState(ctx)
	}
	if err := c.stage.Close(); err != nil {
		_ = local.Close()
		c.setState("unknown", "native-stage-detach-unconfirmed")
		return c.currentState(ctx)
	}
	c.doc.Enabled = false
	c.doc.Phase = "local"
	c.doc.LocalGenerationDirectory = local.root
	c.doc.Outcome = "accepted"
	if err := c.save(); err != nil {
		_ = local.Close()
		c.setState("unknown", "intent-write-unconfirmed")
		return c.currentState(ctx)
	}
	if err := c.installLocal(ctx, local); err != nil {
		_ = local.Close()
		c.doc.Outcome = "unknown"
		_ = c.save()
		c.setState("unknown", "local-start-unconfirmed")
		return c.currentState(ctx)
	}
	c.stage = NodeRoamingStage{}
	c.mu.Lock()
	c.state.Enabled = false
	c.state.ActiveBotNodeID = api.LocalNodeID
	c.state.State = "disabled"
	c.state.Reason = ""
	c.state.Outcome = "accepted"
	c.mu.Unlock()
	return c.currentState(ctx)
}
func (c *nodeRoamingControl) installLocal(ctx context.Context, local *Application) error {
	if err := c.startLocal(ctx, local); err != nil {
		return err
	}
	if err := c.app.Backend.ActivateNodeRoamingLocal(local.Backend); err != nil {
		return err
	}
	c.mu.Lock()
	c.local = local
	c.pendingLocalRestore = false
	c.mu.Unlock()
	return nil
}
func validRoamingStage(stage NodeRoamingStage) bool {
	return stage.Broker != nil && stage.Disable != nil && stage.Close != nil
}
func (c *nodeRoamingControl) recoveryInput() (NodeRoamingRecoveryInput, error) {
	doc, err := loadNodeManagementDocument(filepath.Join(c.app.root, "nodeplane", "config.json"))
	if err != nil {
		return NodeRoamingRecoveryInput{}, err
	}
	regs := append([]NodeRegistration{{ID: api.LocalNodeID, Label: "This machine", Join: api.NodeLocal}}, doc.Nodes...)
	var coordinator NodeRegistration
	for _, reg := range regs {
		if reg.ID == c.doc.CoordinatorNodeID {
			coordinator = reg
		}
	}
	if coordinator.ID == "" {
		return NodeRoamingRecoveryInput{}, errors.New("original coordinator enrollment unavailable")
	}
	sourceBackend := c.doc.SourceBackend
	if sourceBackend == "" {
		sourceBackend = "codex"
	}
	return NodeRoamingRecoveryInput{StageInput: NodeRoamingStageInput{OperationID: c.doc.StageOperationID, BotID: c.doc.BotID, Coordinator: coordinator, Nodes: regs, SourceTarget: api.WorkTarget{NodeID: api.LocalNodeID, Backend: sourceBackend, Role: api.RoleBot}, ReviewedPlanID: c.doc.ReviewedPlanID, AllowPersistentExecution: c.doc.AllowPersistentExecution, Resume: true}, OperationID: c.doc.OperationID, StageOperationID: c.doc.StageOperationID, OperationKind: c.doc.OperationKind, Phase: c.doc.Phase, SourceRetiredIntent: c.doc.SourceRetiredIntent, LocalGenerationDirectory: c.doc.LocalGenerationDirectory}, nil
}
func (c *nodeRoamingControl) recoverOriginal(ctx context.Context) error {
	if c.options.Recover == nil {
		return errors.New("original native roaming recovery unavailable")
	}
	input, err := c.recoveryInput()
	if err != nil {
		return err
	}
	result, err := c.options.Recover(ctx, input)
	if err != nil {
		return err
	}
	if result.OperationID != c.doc.OperationID {
		return errors.New("original roaming recovery identity mismatch")
	}
	if result.Outcome == "unknown" {
		return nil
	}
	switch {
	case result.Outcome == "accepted" && result.Phase == "active" && c.doc.OperationKind == "enable" && validRoamingStage(result.Stage):
		c.doc.Enabled = true
		c.doc.Phase = "active"
		c.doc.SourceRetiredIntent = true
		c.doc.Outcome = "accepted"
		if err := c.save(); err != nil {
			return err
		}
		c.stage = result.Stage
		for _, reg := range input.StageInput.Nodes {
			c.registrations[reg.ID] = reg
		}
		c.mu.Lock()
		c.state.Enabled = true
		c.state.Outcome = "accepted"
		c.state.State = "waiting"
		c.mu.Unlock()
		if err := c.app.Backend.ActivateNodeRoamingProduct(roamingWaitingEngine{}); err != nil {
			return err
		}
		c.app.Backend.ConfigureProductConnection(c)
		c.startObserver()
		_ = c.followOwner(ctx)
	case result.Outcome == "accepted" && result.Phase == "local" && result.Local != nil && result.Local != c.app:
		result.Local.mu.Lock()
		started := result.Local.started
		result.Local.mu.Unlock()
		if started {
			_ = result.Local.Close()
			return errors.New("recovered local candidate already active")
		}
		c.doc.Enabled = false
		c.doc.Phase = "local"
		c.doc.LocalGenerationDirectory = result.Local.root
		c.doc.Outcome = "accepted"
		if err := c.save(); err != nil {
			_ = result.Local.Close()
			return err
		}
		if err := c.installLocal(ctx, result.Local); err != nil {
			_ = result.Local.Close()
			c.setState("unknown", "local-start-unconfirmed")
			_ = c.save()
			return err
		}
		c.mu.Lock()
		c.state.Enabled = false
		c.state.Outcome = "accepted"
		c.state.State = "disabled"
		c.state.ActiveBotNodeID = api.LocalNodeID
		c.state.Reason = ""
		c.mu.Unlock()
	case result.Outcome == "rejected" && result.Phase == "source-active":
		if c.proxy.Current() != c.app.engine {
			if err := c.app.Backend.ActivateNodeRoamingProduct(c.app.engine); err != nil {
				return err
			}
		}
		c.doc.Enabled = false
		c.doc.Phase = "local"
		c.doc.SourceRetiredIntent = false
		c.doc.Outcome = "rejected"
		if err := c.save(); err != nil {
			return err
		}
		c.mu.Lock()
		c.state.Enabled = false
		c.state.Outcome = "rejected"
		c.state.State = "disabled"
		c.state.Reason = "native-preparation-refused"
		c.mu.Unlock()
	default:
		return errors.New("original roaming phase still uncertain")
	}
	return nil
}
func (c *nodeRoamingControl) NodeRoamingState(ctx context.Context) (backend.NodeRoamingState, error) {
	c.op.Lock()
	defer c.op.Unlock()
	if !c.closed && c.doc.Outcome == "unknown" {
		_ = c.recoverOriginal(ctx)
	}
	return c.currentState(ctx)
}
func sameRoamingLease(a, b nodeplane.Lease) bool {
	return a.BotID == b.BotID && a.NodeID == b.NodeID && a.Backend == b.Backend && a.Epoch == b.Epoch && a.Epoch != ""
}
func (c *nodeRoamingControl) followOwner(ctx context.Context) error {
	l, err := c.stage.Broker.CurrentLease(ctx, c.doc.BotID)
	if err != nil || l.Validate() != nil || l.BotID != c.doc.BotID || (l.Backend != api.NodeCodex && l.Backend != api.NodeCaelis) {
		return c.withdrawObserver("broker-owner-unavailable")
	}
	reg, ok := c.registrations[l.NodeID]
	if !ok {
		return c.withdrawObserver("owner-not-enrolled")
	}
	location, err := c.options.ResolveProduct(ctx, l, reg)
	if err != nil || location.Generation == "" || location.Pairing.NodeID != l.NodeID || location.Pairing.BotID != productrpc.ProfileBotID(l.BotID) || validateProductPairing(location.Pairing) != nil {
		return c.withdrawObserver("owner-product-unavailable")
	}
	if sameRoamingLease(l, c.lease) && location.Pairing == c.location.Pairing && location.Generation == c.location.Generation && c.product != nil {
		return nil
	}
	// Every product generation has its own receipt journal. An uncertain command
	// stays with its original scope; changing ownership never sends or queries it
	// against a different generation, even when the node ID remains the same.
	b, _ := json.Marshal([]string{l.BotID, l.NodeID, l.Epoch, location.Generation})
	sum := sha256.Sum256(b)
	root := filepath.Join(c.app.root, "nodeplane", "product-observers", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	factory := c.clientFactory
	if location.ClientFactory != nil {
		factory = location.ClientFactory
	}
	product, err := newProductEngine(root, location.Pairing, factory)
	if err != nil {
		return err
	}
	if err := product.Connect(ctx); err != nil {
		_ = product.Close(ctx)
		return c.withdrawObserver("owner-product-unavailable")
	}
	product.mu.Lock()
	generation := product.identity.Generation
	product.mu.Unlock()
	fresh, err := c.stage.Broker.CurrentLease(ctx, c.doc.BotID)
	if err != nil || fresh.Validate() != nil || !sameRoamingLease(fresh, l) || generation != location.Generation {
		_ = product.Close(ctx)
		return c.withdrawObserver("owner-generation-changed")
	}
	if err := c.app.Backend.ActivateNodeRoamingProduct(product); err != nil {
		_ = product.Close(ctx)
		return err
	}
	old := c.product
	c.product, c.lease, c.location = product, l, location
	if old != nil {
		_ = old.Close(ctx)
	}
	c.mu.Lock()
	c.state.ActiveBotNodeID = l.NodeID
	c.state.State = "ready"
	c.state.Reason = ""
	c.mu.Unlock()
	if c.app.host.Observe != nil {
		c.app.host.Observe(c.app.Backend.Snapshot())
	}
	c.observeProduct(product)
	return nil
}

func (c *nodeRoamingControl) observeProduct(product *productEngine) {
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		var revision uint64
		observer := backend.NotificationObserver{Notify: c.app.host.Notify, Locale: c.app.host.Locale}
		for {
			snapshot, err := product.WaitSnapshot(c.ctx, revision)
			if err != nil || c.ctx.Err() != nil {
				return
			}
			revision = snapshot.Revision
			c.op.Lock()
			current := !c.closed && c.product == product
			c.op.Unlock()
			if !current {
				return
			}
			observer.Observe(snapshot)
			if c.app.host.Observe != nil {
				c.app.host.Observe(c.app.Backend.Snapshot())
			}
			summaries := product.TaskSummaries()
			if c.app.host.ObserveTaskReceipts != nil {
				c.app.host.ObserveTaskReceipts(summaries)
			}
			if c.app.host.ObserveTasks != nil {
				previews := make([]api.TaskPreview, 0, len(summaries))
				for _, task := range summaries {
					previews = append(previews, api.TaskPreview{ID: task.ID, Prompt: task.Title, Status: task.Status, Provider: "remote-product", Locked: task.Locked})
				}
				c.app.host.ObserveTasks(previews)
			}
		}
	}()
}
func (c *nodeRoamingControl) withdrawObserver(reason string) error {
	if err := c.app.Backend.ActivateNodeRoamingProduct(roamingWaitingEngine{}); err != nil {
		return err
	}
	if c.product != nil {
		_ = c.product.Close(context.Background())
		c.product = nil
	}
	c.mu.Lock()
	c.state.ActiveBotNodeID = ""
	c.state.State = "waiting"
	c.state.Reason = reason
	c.mu.Unlock()
	return errors.New(reason)
}
func (c *nodeRoamingControl) startObserver() {
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		ticker := time.NewTicker(nodeplane.DefaultHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-ticker.C:
				c.op.Lock()
				if c.doc.Enabled && c.doc.Phase == "active" && !c.closed && c.stage.Broker != nil {
					_ = c.followOwner(c.ctx)
				}
				c.op.Unlock()
			}
		}
	}()
}
func (c *nodeRoamingControl) Close() error {
	c.op.Lock()
	if c.closed {
		c.op.Unlock()
		return nil
	}
	c.closed = true
	c.cancel()
	c.op.Unlock()
	c.workers.Wait()
	c.op.Lock()
	defer c.op.Unlock()
	var err error
	if c.product != nil {
		err = c.product.Close(context.Background())
	}
	if c.local != nil {
		err = errors.Join(err, c.local.Close())
	}
	if c.stage.Close != nil {
		err = errors.Join(err, c.stage.Close())
	}
	return err
}

type roamingWaitingEngine struct{}

func (roamingWaitingEngine) Connect(context.Context) error {
	return errors.New("awaiting verified native owner")
}
func (roamingWaitingEngine) Snapshot() api.Snapshot {
	return api.Snapshot{Connection: "offline", ConnectionIssue: "roaming-owner-unavailable", Phase: "idle"}
}
func (roamingWaitingEngine) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	return api.Receipt{ID: in.ID, Outcome: "rejected"}, errors.New("awaiting verified native owner")
}
func (roamingWaitingEngine) Interrupt(context.Context) error { return errors.New("no verified owner") }
func (roamingWaitingEngine) Decide(context.Context, api.Decision) error {
	return errors.New("no verified owner")
}
func (roamingWaitingEngine) Close(context.Context) error { return nil }
func (roamingWaitingEngine) Draft() api.Draft            { return api.Draft{} }
func (roamingWaitingEngine) SaveDraft(api.Draft) (api.Draft, error) {
	return api.Draft{}, errors.New("awaiting verified native owner")
}

func (c *nodeRoamingControl) PrepareNodeRoaming(ctx context.Context, r backend.NodeRoamingRequest) (backend.NodeRoamingPlan, error) {
	c.op.Lock()
	defer c.op.Unlock()
	if c.closed || c.options.PreparePlan == nil {
		return backend.NodeRoamingPlan{}, errors.New("native deployment planning unavailable")
	}
	if c.doc.Outcome == "unknown" {
		return backend.NodeRoamingPlan{}, errors.New("original roaming operation requires native reconciliation")
	}
	input, err := c.input(ctx, r)
	if err != nil {
		return backend.NodeRoamingPlan{}, err
	}
	plan, err := c.options.PreparePlan(ctx, input)
	if err == nil && (plan.ID == "" || plan.CoordinatorNodeID != input.Coordinator.ID) {
		return backend.NodeRoamingPlan{}, errors.New("native deployment plan scope mismatch")
	}
	return plan, err
}

func (c *nodeRoamingControl) ConnectionState() backend.ProductConnectionState {
	c.mu.Lock()
	state := c.state
	c.mu.Unlock()
	return backend.ProductConnectionState{Revision: c.proxy.Revision(), ActiveMode: "remote", Pairing: backend.ProductPairing{Mode: "remote"}, State: state.State, Issue: state.Reason}
}
func (c *nodeRoamingControl) SavePairing(backend.ProductPairing, uint64) (backend.ProductConnectionState, error) {
	return c.ConnectionState(), errors.New("disable automatic roaming before replacing product pairing")
}
func (c *nodeRoamingControl) Reconnect(ctx context.Context) error {
	c.op.Lock()
	defer c.op.Unlock()
	if !c.doc.Enabled || c.doc.Phase != "active" || c.doc.Outcome == "unknown" || c.stage.Broker == nil {
		return errors.New("automatic roaming unavailable")
	}
	return c.followOwner(ctx)
}
func (c *nodeRoamingControl) Disconnect(ctx context.Context) error {
	c.op.Lock()
	defer c.op.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = c.withdrawObserver("observer-detached")
	return nil
}

// ActiveNodeRoamingApplication gives native APP actions the fresh local owner
// after disable. During automatic roaming the stable Backend is the product
// observer; the retired source must not service native task/setup operations.
func ActiveNodeRoamingApplication(a *Application) *Application {
	if a == nil || a.Backend == nil {
		return a
	}
	port, err := backend.NativeNodeRoamingController(a.Backend)
	if err != nil {
		return a
	}
	c, ok := port.(*nodeRoamingControl)
	if !ok {
		return a
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.local != nil {
		return c.local
	}
	return a
}
func NodeRoamingOwnsExecution(a *Application) bool {
	if a == nil || a.Backend == nil {
		return false
	}
	port, err := backend.NativeNodeRoamingController(a.Backend)
	if err != nil {
		return false
	}
	c, ok := port.(*nodeRoamingControl)
	if !ok {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.Enabled || c.state.Outcome == "unknown" || c.local != nil || c.pendingLocalRestore
}

// GuardNodeRoamingCoordinator protects native configuration even when callers
// bypass the settings facade. Selection remains an independent read-only view.
func GuardNodeRoamingCoordinator(a *Application, nodeID string) error {
	if a == nil || a.Backend == nil {
		return nil
	}
	port, err := backend.NativeNodeRoamingController(a.Backend)
	if err != nil {
		return nil
	}
	c, ok := port.(*nodeRoamingControl)
	if !ok {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if (c.state.Enabled || c.state.Outcome == "unknown") && nodeID != c.state.CoordinatorNodeID {
		return errors.New("disable automatic roaming before changing its coordinator")
	}
	return nil
}
