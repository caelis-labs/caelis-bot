package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

type controlLocalEngine struct{ submits, closed atomic.Int32 }

func (e *controlLocalEngine) Connect(context.Context) error { return nil }
func (e *controlLocalEngine) Snapshot() api.Snapshot {
	return api.Snapshot{Connection: "ready", CanSend: true, Revision: 1}
}
func (e *controlLocalEngine) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	e.submits.Add(1)
	return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
}
func (*controlLocalEngine) Interrupt(context.Context) error            { return nil }
func (*controlLocalEngine) Decide(context.Context, api.Decision) error { return nil }
func (e *controlLocalEngine) Close(context.Context) error              { e.closed.Add(1); return nil }
func (*controlLocalEngine) ProviderInfo() api.ProviderInfo             { return api.ProviderInfo{ID: "codex"} }

type controlBroker struct {
	mu    sync.Mutex
	lease nodeplane.Lease
	err   error
}

func (b *controlBroker) CurrentLease(context.Context, string) (nodeplane.Lease, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lease, b.err
}

type controlFixture struct {
	a                                                  *Application
	c                                                  *nodeRoamingControl
	broker                                             *controlBroker
	client                                             *thinClientFixture
	local                                              *controlLocalEngine
	preflight, prepared, staged, disabled, stageClosed atomic.Int32
	preflightErr, stageErr, disableErr                 error
}

func roamingControlFixture(t *testing.T) *controlFixture {
	t.Helper()
	f := &controlFixture{local: &controlLocalEngine{}, client: newThinClientFixture(), broker: &controlBroker{lease: nodeplane.Lease{BotID: "bot-fixture", NodeID: "node-fixture", Backend: api.NodeCodex, Epoch: "1", ExpiresAt: time.Now().Add(time.Minute), TTLMs: 60000}}}
	f.client.state.BotID = productrpc.ProfileBotID("bot-fixture")
	f.a = &Application{root: t.TempDir(), engine: f.local, Backend: backend.NewService(f.local, func([]string) ([]api.InputFile, error) { return nil, nil }, func([]string) {}, nil, nil)}
	agent := newNodeManagementFixture()
	agent.catalog.Broker = &api.NodeBroker{NodeID: "local", Reachable: true}
	agent.catalog.Nodes[1].ID = "node-fixture"
	agent.catalog.Nodes[1].Join = api.NodeSSH
	f.a.Backend.SetNodeManagementController(NewNodeManagement(agent, nil))
	if err := localstate.Write(filepath.Join(f.a.root, "nodeplane", "config.json"), nodeManagementDocument{Version: 1, Revision: 1, Coordinator: "local", Nodes: []NodeRegistration{{ID: "node-fixture", Label: "Fixture", Join: api.NodeSSH, SSHDestination: "fixture-target", Directory: "/fixture/profile", HelperPath: "/fixture/helper"}}}); err != nil {
		t.Fatal(err)
	}
	o := NodeRoamingOptions{
		PreparePlan: func(context.Context, NodeRoamingStageInput) (backend.NodeRoamingPlan, error) {
			return backend.NodeRoamingPlan{ID: "reviewed-plan", CoordinatorNodeID: "local", RequiresConfirmation: true}, nil
		},
		Preflight: func(context.Context, NodeRoamingStageInput) error { f.preflight.Add(1); return f.preflightErr },
		Stage: func(ctx context.Context, in NodeRoamingStageInput) (NodeRoamingStage, error) {
			f.staged.Add(1)
			if !in.Resume {
				proof, err := in.Source.ReadRuntimeProof(ctx, in.SourceTarget)
				if err != nil || proof.Snapshot != in.Snapshot || !proof.SafeIdle {
					t.Error("missing concrete stopped source proof")
				}
			}
			return NodeRoamingStage{Broker: f.broker, Disable: func(context.Context, string) error { f.disabled.Add(1); return f.disableErr }, Close: func() error { f.stageClosed.Add(1); return nil }}, f.stageErr
		},
		ResolveProduct: func(context.Context, nodeplane.Lease, NodeRegistration) (NodeRoamingProductLocation, error) {
			f.client.mu.Lock()
			gen := f.client.state.Generation
			f.client.mu.Unlock()
			pairing := thinPairing()
			pairing.BotID = productrpc.ProfileBotID("bot-fixture")
			return NodeRoamingProductLocation{Pairing: pairing, Generation: gen}, nil
		},
		RestoreLocal: func(context.Context, NodeRoamingStage) (*Application, error) {
			engine := &controlLocalEngine{}
			return &Application{root: t.TempDir(), engine: engine, Backend: backend.NewService(engine, nil, nil, nil, nil)}, nil
		},
	}
	if err := AttachNodeRoaming(f.a, o); err != nil {
		t.Fatal(err)
	}
	port, err := backend.NativeNodeRoamingController(f.a.Backend)
	if err != nil {
		t.Fatal(err)
	}
	f.c = port.(*nodeRoamingControl)
	f.c.startLocal = func(_ context.Context, a *Application) error {
		if f.c.doc.Phase != "local" || f.c.doc.Outcome != "accepted" {
			t.Error("local admission opened before durable disable")
		}
		saved, err := loadNodeRoamingDocument(f.c.filename())
		if err != nil || saved.Enabled || saved.Outcome != "accepted" {
			t.Error("local admission opened before accepted durable barrier", err)
		}
		a.mu.Lock()
		a.started = true
		a.mu.Unlock()
		return nil
	}
	f.c.prepareLocalSource = func(ctx context.Context, a *Application, id string) ([]byte, nodeplane.SnapshotRef, nodeplane.RuntimeProofPort, error) {
		if a == f.a {
			t.Error("retired original source selected")
		}
		return f.c.prepareSource(ctx, id)
	}
	f.c.prepareSource = func(ctx context.Context, id string) ([]byte, nodeplane.SnapshotRef, nodeplane.RuntimeProofPort, error) {
		f.prepared.Add(1)
		ref := nodeplane.SnapshotRef{BotID: "bot-fixture", Epoch: "0", Version: "1", Digest: "fixture"}
		target := api.WorkTarget{NodeID: id, Backend: "codex", Role: api.RoleBot}
		return []byte("fixture-only"), ref, &preparedNotebookSource{target: target, ref: ref, generation: "source-generation"}, nil
	}
	f.c.clientFactory = func(backend.ProductPairing) (nativeProductClient, io.Closer, error) {
		return f.client, &thinCloserFixture{}, nil
	}
	t.Cleanup(func() { _ = f.c.Close() })
	return f
}
func controlRequest(id string) backend.NodeRoamingRequest {
	return backend.NodeRoamingRequest{ReviewedPlanID: "reviewed-plan", AllowPersistentExecution: true, ID: id, ExpectedCatalogRevision: "catalog-1"}
}

func TestNodeRoamingControlSelectionAndStaleEnablePreserveOriginalLocal(t *testing.T) {
	f := roamingControlFixture(t)
	if _, err := f.a.Backend.SelectNode(t.Context(), "node-fixture", "catalog-1"); err != nil {
		t.Fatal(err)
	}
	state, err := f.a.Backend.NodeRoamingState(t.Context())
	if err != nil || state.Enabled || f.prepared.Load() != 0 || f.staged.Load() != 0 {
		t.Fatal(state, err)
	}
	r := controlRequest("enable-original")
	r.ExpectedCatalogRevision = "stale"
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), r); err == nil {
		t.Fatal("stale enable accepted")
	}
	if f.prepared.Load() != 0 || f.staged.Load() != 0 || f.a.Backend.ProviderInfo().ID != "codex" {
		t.Fatal("view changed execution")
	}
	if _, err := f.a.Backend.Submit(t.Context(), api.Submission{ID: "local", Text: "local"}); err != nil || f.local.submits.Load() != 1 {
		t.Fatal("default submit lost", err)
	}
}
func TestNodeRoamingControlPreflightFailureDoesNotRetireSource(t *testing.T) {
	f := roamingControlFixture(t)
	f.preflightErr = errors.New("candidate route unavailable")
	_, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original"))
	if err == nil {
		t.Fatal("failed preflight accepted")
	}
	if f.prepared.Load() != 0 || f.staged.Load() != 0 || f.local.closed.Load() != 0 {
		t.Fatal("source retired before native preparation")
	}
}

func TestNodeRoamingControlPlanPreparationRequiresExplicitReviewBeforeRetirement(t *testing.T) {
	f := roamingControlFixture(t)
	r := controlRequest("enable-original")
	plan, err := f.a.Backend.PrepareNodeRoaming(t.Context(), r)
	if err != nil || plan.ID != "reviewed-plan" || !plan.RequiresConfirmation || f.prepared.Load() != 0 || f.preflight.Load() != 0 || f.staged.Load() != 0 {
		t.Fatal("plan preparation mutated lifecycle", plan, err)
	}
	r.AllowPersistentExecution = false
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), r); err == nil {
		t.Fatal("plan executed without review confirmation")
	}
	r.AllowPersistentExecution = true
	r.ReviewedPlanID = "stale-reviewed-plan"
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), r); err == nil {
		t.Fatal("stale plan executed")
	}
	if f.prepared.Load() != 0 || f.preflight.Load() != 0 || f.staged.Load() != 0 {
		t.Fatal("unreviewed plan retired source")
	}
}

func TestNodeRoamingControlSavedUnknownNeverStartsOriginalSource(t *testing.T) {
	f := roamingControlFixture(t)
	f.c.doc = nodeRoamingDocument{Phase: "staging", OperationKind: "enable", StageOperationID: "original-unknown", SourceRetiredIntent: true, ReviewedPlanID: "reviewed-plan", AllowPersistentExecution: true, Version: 1, OperationID: "original-unknown", Outcome: "unknown", CoordinatorNodeID: "local", BotID: "bot-fixture"}
	if err := f.c.save(); err != nil {
		t.Fatal(err)
	}
	engine := &controlLocalEngine{}
	a := &Application{root: f.a.root, engine: engine, Backend: backend.NewService(engine, nil, nil, nil, nil)}
	agent := newNodeManagementFixture()
	agent.catalog.Broker = &api.NodeBroker{NodeID: "local", Reachable: true}
	a.Backend.SetNodeManagementController(NewNodeManagement(agent, nil))
	if err := AttachNodeRoaming(a, f.c.options); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Backend.CloseNodeManagement(context.Background()) })
	owned, err := RestoreNodeRoaming(t.Context(), a)
	if !owned || err == nil || !NodeRoamingOwnsExecution(a) || f.staged.Load() != 0 || f.prepared.Load() != 0 {
		t.Fatal("unknown restart revived source or replayed native stage", owned, err)
	}
}
func TestNodeRoamingControlEnableUsesConcreteStageAndStableService(t *testing.T) {
	f := roamingControlFixture(t)
	service := f.a.Backend
	state, err := service.EnableNodeRoaming(t.Context(), controlRequest("enable-original"))
	if err != nil || !state.Enabled || state.State != "ready" || state.ActiveBotNodeID != "node-fixture" || state.OperationID != "enable-original" || state.Outcome != "accepted" {
		t.Fatal(state, err)
	}
	if f.a.Backend != service || f.a.engine != f.local || f.prepared.Load() != 1 || f.staged.Load() != 1 {
		t.Fatal("APP source/service identity replaced")
	}
	_, err = service.Submit(t.Context(), api.Submission{ID: "remote-request", Text: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if f.local.submits.Load() != 0 || len(f.client.commands) != 1 {
		t.Fatal("submission did not reach active product")
	}
}
func TestNodeRoamingControlUnknownStageRetainsOriginalOperationWithoutReplay(t *testing.T) {
	f := roamingControlFixture(t)
	f.stageErr = errors.New("stage result uncertain")
	state, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original"))
	if err != nil || state.Outcome != "unknown" || state.OperationID != "enable-original" {
		t.Fatal(state, err)
	}
	_, err = f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("replacement"))
	if err == nil || f.staged.Load() != 1 || f.prepared.Load() != 1 {
		t.Fatal("unknown enable replayed", err)
	}
	state, err = f.a.Backend.NodeRoamingState(t.Context())
	if err != nil || state.OperationID != "enable-original" || state.Outcome != "unknown" {
		t.Fatal(state, err)
	}
}
func TestNodeRoamingControlGenerationChangeRetainsUnknownOriginalReceipt(t *testing.T) {
	f := roamingControlFixture(t)
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); err != nil {
		t.Fatal(err)
	}
	old := f.c.product
	f.client.command = func(_ context.Context, in productrpc.Command) (productrpc.Result, error) {
		return productrpc.Result{ID: in.ID, Outcome: "unknown"}, errors.New("lost result")
	}
	_, _ = f.a.Backend.Submit(t.Context(), api.Submission{ID: "old-original", Text: "fixture"})
	fresh := newThinClientFixture()
	fresh.state.BotID = productrpc.ProfileBotID("bot-fixture")
	fresh.state.Generation = "generation-two"
	fresh.state.Cursor.Generation = "generation-two"
	f.client = fresh
	f.c.op.Lock()
	err := f.c.followOwner(t.Context())
	f.c.op.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if f.c.product == old || f.c.product.root == old.root {
		t.Fatal("generation reused original receipt scope")
	}
	if len(fresh.commands) != 0 || len(fresh.lookups) != 0 {
		t.Fatal("old uncertain operation crossed generation")
	}
	b, err := os.ReadFile(filepath.Join(old.root, "product-client-receipts.json"))
	if err != nil || len(b) == 0 {
		t.Fatal("original uncertain journal lost", err)
	}
	if _, err := f.a.Backend.Submit(t.Context(), api.Submission{ID: "new-original", Text: "fixture"}); err != nil {
		t.Fatal("old uncertainty blocked independent new generation", err)
	}
}
func TestNodeRoamingControlDisableBusyRefusesThenRestoresFreshLocal(t *testing.T) {
	f := roamingControlFixture(t)
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); err != nil {
		t.Fatal(err)
	}
	f.disableErr = ErrNodeRoamingBusy
	state, err := f.a.Backend.DisableNodeRoaming(t.Context(), controlRequest("disable-original"))
	if err != nil || !state.Enabled || state.Outcome != "rejected" || f.c.local != nil {
		t.Fatal(state, err)
	}
	f.disableErr = nil
	state, err = f.a.Backend.DisableNodeRoaming(t.Context(), controlRequest("disable-next"))
	if err != nil || state.Enabled || state.State != "disabled" || state.ActiveBotNodeID != "local" || f.c.local == nil || f.c.local == f.a {
		t.Fatal(state, err)
	}
	if f.c.proxy.Current() == f.local || f.local.submits.Load() != 0 {
		t.Fatal("original source runtime revived")
	}
}
func TestNodeRoamingControlUnenrolledOwnerCannotChooseProduct(t *testing.T) {
	f := roamingControlFixture(t)
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); err != nil {
		t.Fatal(err)
	}
	f.broker.mu.Lock()
	f.broker.lease.NodeID = "foreign"
	f.broker.mu.Unlock()
	f.c.op.Lock()
	err := f.c.followOwner(t.Context())
	f.c.op.Unlock()
	if err == nil || f.a.Backend.Snapshot().CanSend {
		t.Fatal("foreign owner gained a route", err)
	}
}
