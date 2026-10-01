package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

// Stage input is native-only. Registrations come from the enrolled document,
// and Source is the original concrete APP's immutable stopped-owner proof.
var ErrNodeRoamingBusy = errors.New("native roaming owner has active or uncertain work")
var ErrNodeRoamingPreflight = errors.New("native preparation refused before source retirement")

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
	Disable func(context.Context) error
	Close   func() error
}
type NodeRoamingProductLocation struct {
	ClientFactory productClientFactory
	Pairing       backend.ProductPairing
	Generation    string
}
type NodeRoamingOptions struct {
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
	op            sync.Mutex
	mu            sync.Mutex
	app           *Application
	options       NodeRoamingOptions
	proxy         *backend.NodeRoamingEngine
	doc           nodeRoamingDocument
	state         backend.NodeRoamingState
	stage         NodeRoamingStage
	registrations map[string]NodeRegistration
	product       *productEngine
	lease         nodeplane.Lease
	location      NodeRoamingProductLocation
	local         *Application
	ctx           context.Context
	cancel        context.CancelFunc
	workers       sync.WaitGroup
	closed        bool
	prepareSource func(context.Context, string) ([]byte, nodeplane.SnapshotRef, nodeplane.RuntimeProofPort, error)
	clientFactory productClientFactory
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
	proxy, err := a.Backend.PrepareNodeRoamingEngine()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &nodeRoamingControl{app: a, options: o, proxy: proxy, ctx: ctx, cancel: cancel, prepareSource: a.PrepareRoamingBootstrap, clientFactory: newSSHProductClient, doc: nodeRoamingDocument{Version: 1}, registrations: map[string]NodeRegistration{}}
	c.state = backend.NodeRoamingState{Available: o.PreparePlan != nil && o.Preflight != nil && o.Stage != nil && o.ResolveProduct != nil && o.RestoreLocal != nil, State: "disabled"}
	b, err := os.ReadFile(c.filename())
	if err == nil {
		if json.Unmarshal(b, &c.doc) != nil || c.doc.Version != 1 {
			cancel()
			return errors.New("saved roaming intent invalid")
		}
		c.state.Enabled = c.doc.Enabled
		c.state.CoordinatorNodeID = c.doc.CoordinatorNodeID
		c.state.OperationID = c.doc.OperationID
		c.state.Outcome = c.doc.Outcome
		if c.doc.Enabled {
			c.state.State = "waiting"
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		cancel()
		return err
	}
	if err := a.Backend.ConfigureNodeRoaming(c); err != nil {
		cancel()
		return err
	}
	return nil
}
func (c *nodeRoamingControl) filename() string {
	return filepath.Join(c.app.root, "nodeplane", "roaming.json")
}

// RestoreNodeRoaming is invoked by native startup before original APP Start.
// Only durable explicit enablement can start the staged native hosts again.
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
	if !c.doc.Enabled {
		return false, nil
	}
	if !c.state.Available || c.doc.BotID == "" {
		return true, errors.New("saved native roaming assembly unavailable")
	}
	catalog, err := a.Backend.NodeCatalog(ctx)
	if err != nil {
		return true, err
	}
	input, err := c.input(ctx, backend.NodeRoamingRequest{ID: c.doc.OperationID, ExpectedCatalogRevision: catalog.Revision})
	if err != nil {
		return true, err
	}
	if input.Coordinator.ID != c.doc.CoordinatorNodeID {
		return true, errors.New("saved coordinator changed")
	}
	input.Resume = true
	input.ReviewedPlanID = c.doc.ReviewedPlanID
	input.AllowPersistentExecution = c.doc.AllowPersistentExecution
	input.BotID = c.doc.BotID
	if err := c.options.Preflight(ctx, input); err != nil {
		return true, err
	}
	if err := a.Backend.PrepareRestart(func() error { return nil }); err != nil {
		return true, err
	}
	stage, err := c.options.Stage(ctx, input)
	if err != nil || stage.Broker == nil || stage.Disable == nil || stage.Close == nil {
		c.setState("unknown", "native-resume-unconfirmed")
		return true, errors.New("saved native roaming could not resume")
	}
	c.stage = stage
	for _, reg := range input.Nodes {
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
	c.app.Backend.ConfigureProductConnection(c)
	c.app.Backend.ConfigureProductConnection(c)
	c.startObserver()
	_ = c.followOwner(ctx)
	return true, nil
}
func (c *nodeRoamingControl) save() error { return localstate.Write(c.filename(), c.doc) }
func (c *nodeRoamingControl) NodeRoamingState(ctx context.Context) (backend.NodeRoamingState, error) {
	if err := ctx.Err(); err != nil {
		return backend.NodeRoamingState{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state, nil
}
func (c *nodeRoamingControl) setState(state, reason string) {
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
	return NodeRoamingStageInput{ReviewedPlanID: r.ReviewedPlanID, AllowPersistentExecution: r.AllowPersistentExecution, OperationID: r.ID, Coordinator: coordinator, Nodes: regs, SourceTarget: api.WorkTarget{NodeID: api.LocalNodeID, Backend: "codex", Role: api.RoleBot}}, nil
}
func (c *nodeRoamingControl) EnableNodeRoaming(ctx context.Context, r backend.NodeRoamingRequest) (backend.NodeRoamingState, error) {
	c.op.Lock()
	defer c.op.Unlock()
	state, _ := c.NodeRoamingState(ctx)
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
	c.doc = nodeRoamingDocument{ReviewedPlanID: r.ReviewedPlanID, AllowPersistentExecution: r.AllowPersistentExecution, Version: 1, CoordinatorNodeID: input.Coordinator.ID, OperationID: r.ID, Outcome: "unknown"}
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
			_ = c.save()
			c.mu.Lock()
			c.state.Outcome = "rejected"
			c.state.State = "disabled"
			c.state.Reason = "native-preparation-refused"
			c.mu.Unlock()
		} else {
			c.setState("unknown", "native-preparation-unconfirmed")
		}
		result, _ := c.NodeRoamingState(context.Background())
		return result, err
	}
	input.Payload, input.Snapshot, input.Source, err = c.prepareSource(ctx, input.SourceTarget.NodeID)
	if err != nil {
		c.setState("unknown", "source-retirement-unconfirmed")
		return c.NodeRoamingState(ctx)
	}
	c.doc.BotID = input.Snapshot.BotID
	input.BotID = c.doc.BotID
	if err := c.save(); err != nil {
		c.setState("unknown", "intent-write-unconfirmed")
		return c.NodeRoamingState(ctx)
	}
	stage, err := c.options.Stage(ctx, input)
	if err != nil || stage.Broker == nil || stage.Disable == nil || stage.Close == nil {
		c.setState("unknown", "native-stage-unconfirmed")
		return c.NodeRoamingState(ctx)
	}
	c.stage = stage
	for _, reg := range input.Nodes {
		c.registrations[reg.ID] = reg
	}
	if c.options.RefreshNodeManagement != nil {
		if err := AttachNodeManagement(c.app, *c.options.RefreshNodeManagement); err != nil {
			c.setState("unknown", "native-catalog-unavailable")
			return c.NodeRoamingState(ctx)
		}
		if err := c.app.Backend.ConfigureNodeRoaming(c); err != nil {
			return state, err
		}
	}
	c.doc.Enabled = true
	c.doc.Outcome = "accepted"
	if err := c.save(); err != nil {
		c.setState("unknown", "intent-write-unconfirmed")
		return c.NodeRoamingState(ctx)
	}
	c.mu.Lock()
	c.state.Enabled = true
	c.state.Outcome = "accepted"
	c.state.State = "waiting"
	c.mu.Unlock()
	if err := c.app.Backend.ActivateNodeRoamingProduct(roamingWaitingEngine{}); err != nil {
		return state, err
	}
	c.startObserver()
	_ = c.followOwner(ctx)
	return c.NodeRoamingState(ctx)
}
func (c *nodeRoamingControl) DisableNodeRoaming(ctx context.Context, r backend.NodeRoamingRequest) (backend.NodeRoamingState, error) {
	c.op.Lock()
	defer c.op.Unlock()
	state, _ := c.NodeRoamingState(ctx)
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
	c.doc.Outcome = "unknown"
	c.mu.Lock()
	c.state.OperationID = r.ID
	c.state.Outcome = "unknown"
	c.state.State = "disabling"
	c.mu.Unlock()
	if err := c.save(); err != nil {
		c.setState("unknown", "intent-write-unconfirmed")
		return c.NodeRoamingState(ctx)
	}
	if err := c.stage.Disable(ctx); err != nil {
		if errors.Is(err, ErrNodeRoamingBusy) {
			c.doc.Outcome = "rejected"
			if saveErr := c.save(); saveErr != nil {
				c.setState("unknown", "intent-write-unconfirmed")
				return c.NodeRoamingState(ctx)
			}
			c.mu.Lock()
			c.state.State = state.State
			c.state.Reason = "owner-not-safe-idle"
			c.state.Outcome = "rejected"
			c.mu.Unlock()
		} else {
			c.setState("unknown", "native-disable-unconfirmed")
		}
		return c.NodeRoamingState(ctx)
	}
	local, err := c.options.RestoreLocal(ctx, c.stage)
	if err != nil || local == nil || local == c.app || local.Backend == nil {
		c.setState("unknown", "local-restore-unconfirmed")
		return c.NodeRoamingState(ctx)
	}
	if err := local.Start(); err != nil {
		_ = local.Close()
		c.setState("unknown", "local-start-unconfirmed")
		return c.NodeRoamingState(ctx)
	}
	if err := c.app.Backend.ActivateNodeRoamingLocal(local.Backend); err != nil {
		_ = local.Close()
		return state, err
	}
	c.local = local
	if c.product != nil {
		_ = c.product.Close(ctx)
		c.product = nil
	}
	if err := c.stage.Close(); err != nil {
		c.setState("unknown", "native-stage-close-unconfirmed")
		return c.NodeRoamingState(ctx)
	}
	c.stage = NodeRoamingStage{}
	c.doc.Enabled = false
	c.doc.Outcome = "accepted"
	if err := c.save(); err != nil {
		c.setState("unknown", "intent-write-unconfirmed")
		return c.NodeRoamingState(ctx)
	}
	c.mu.Lock()
	c.state.Enabled = false
	c.state.ActiveBotNodeID = api.LocalNodeID
	c.state.State = "disabled"
	c.state.Reason = ""
	c.state.Outcome = "accepted"
	c.mu.Unlock()
	return c.NodeRoamingState(ctx)
}
func sameRoamingLease(a, b nodeplane.Lease) bool {
	return a.BotID == b.BotID && a.NodeID == b.NodeID && a.Backend == b.Backend && a.Epoch == b.Epoch && a.Epoch != ""
}
func (c *nodeRoamingControl) followOwner(ctx context.Context) error {
	l, err := c.stage.Broker.CurrentLease(ctx, c.doc.BotID)
	if err != nil || l.Validate() != nil || l.BotID != c.doc.BotID || l.Backend != api.NodeCodex {
		return c.withdrawObserver("broker-owner-unavailable")
	}
	reg, ok := c.registrations[l.NodeID]
	if !ok {
		return c.withdrawObserver("owner-not-enrolled")
	}
	location, err := c.options.ResolveProduct(ctx, l, reg)
	if err != nil || location.Generation == "" || location.Pairing.NodeID != l.NodeID || location.Pairing.BotID != l.BotID || validateProductPairing(location.Pairing) != nil {
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
				if c.doc.Enabled && !c.closed && c.stage.Broker != nil {
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
	if !c.doc.Enabled || c.stage.Broker == nil {
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
