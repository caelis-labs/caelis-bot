package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

type nodeManagementFixture struct {
	mu                          sync.Mutex
	catalog                     api.NodeCatalog
	manageCalls, reconcileCalls int
	manage                      func(nodeplane.ManagementRequest) (api.NodeOperationReceipt, error)
	configured                  *api.NodeRuntimeConfiguration
}

func newNodeManagementFixture() *nodeManagementFixture {
	worker := &api.WorkTarget{NodeID: "local", Backend: "caelis", Role: api.RoleWorker}
	f := &nodeManagementFixture{catalog: api.NodeCatalog{Revision: "catalog-1", SelectedNodeID: "local", ActiveBotNodeID: "local", WorkerTarget: worker}}
	for _, id := range []string{"local", "other"} {
		join := api.NodeLocal
		if id == "other" {
			join = api.NodeOutgoing
		}
		f.catalog.Nodes = append(f.catalog.Nodes, api.NodeInfo{ID: id, Label: id, OS: api.NodeLinux, Join: join, Runtimes: []api.NodeRuntime{{Backend: api.NodeCaelis, Authentication: api.NodeAuthenticated, Health: api.NodeHealthy, Roles: []api.NodeRoleCapability{{Role: api.RoleWorker, Eligible: true}}}}})
	}
	return f
}

func (f *nodeManagementFixture) Catalog(context.Context) (api.NodeCatalog, error) {
	return f.catalog, nil
}
func (f *nodeManagementFixture) Configuration(_ context.Context, node string, backend api.NodeBackend) (api.NodeRuntimeConfiguration, error) {
	if f.configured != nil {
		return *f.configured, nil
	}
	return api.NodeRuntimeConfiguration{Guard: api.NodeEditGuard{NodeID: node, Backend: backend, Revision: "7"}, Configuration: api.RuntimeConfiguration{Revision: "7"}}, nil
}
func (f *nodeManagementFixture) Manage(_ context.Context, r nodeplane.ManagementRequest) (api.NodeOperationReceipt, error) {
	f.mu.Lock()
	f.manageCalls++
	f.mu.Unlock()
	if f.manage != nil {
		return f.manage(r)
	}
	return api.NodeOperationReceipt{}, errors.New("lost response")
}
func (f *nodeManagementFixture) Reconcile(_ context.Context, ref api.NodeOperationRef) (api.NodeOperationReceipt, error) {
	f.mu.Lock()
	f.reconcileCalls++
	f.mu.Unlock()
	return api.NodeOperationReceipt{Ref: ref, Outcome: api.NodeCommitted, Revision: "8"}, nil
}

func nodeIntent(t *testing.T, node, id string) api.NodeManagementRequest {
	t.Helper()
	r := api.NodeManagementRequest{Guard: api.NodeEditGuard{NodeID: node, Backend: api.NodeCaelis, Revision: "7"}, Ref: api.NodeOperationRef{NodeID: node, Backend: api.NodeCaelis, OperationID: id}, Change: &api.RuntimeConfigurationChange{Action: "main", ExpectedRevision: "7", Selection: api.WorkExecutionSettings{Model: "selected-model"}}}
	digest, err := nodeplane.ManagementDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Ref.RequestDigest = digest
	return r
}

func TestNodeManagementUnknownSurvivesSelectionWithoutResend(t *testing.T) {
	ctx := context.Background()
	f := newNodeManagementFixture()
	m := NewNodeManagement(f, nil)
	original := nodeIntent(t, "local", "original")
	result, err := m.ChangeNodeConfiguration(ctx, original)
	if err != nil || result.Outcome != api.NodeUnknown || result.Ref != original.Ref {
		t.Fatal("lost response did not retain original reference", result, err)
	}
	selected, err := m.SelectNode(ctx, "other", "catalog-1")
	if err != nil || selected.SelectedNodeID != "other" || selected.ActiveBotNodeID != "local" || *selected.WorkerTarget != *f.catalog.WorkerTarget {
		t.Fatal("view selection changed execution", selected, err)
	}
	if _, err = m.ChangeNodeConfiguration(ctx, original); err != nil {
		t.Fatal(err)
	}
	if _, err = m.ChangeNodeConfiguration(ctx, nodeIntent(t, "local", "replacement")); err == nil {
		t.Fatal("new ID bypassed original unknown outcome")
	}
	if _, err = m.ChangeNodeConfiguration(ctx, nodeIntent(t, "other", "independent")); err != nil {
		t.Fatal("pending local operation blocked another node", err)
	}
	result, err = m.ReconcileNodeOperation(ctx, original.Ref)
	if err != nil || result.Outcome != api.NodeCommitted || result.Ref != original.Ref {
		t.Fatal("original receipt recovery failed", result, err)
	}
	f.mu.Lock()
	calls, recovery := f.manageCalls, f.reconcileCalls
	f.mu.Unlock()
	if calls != 2 || recovery != 1 {
		t.Fatal("original mutation resent", calls, recovery)
	}
	c, err := m.NodeCatalog(ctx)
	if err != nil || c.SelectedNodeID != "other" || c.ActiveBotNodeID != "local" {
		t.Fatal("late receipt replaced selected view", c, err)
	}
}

func TestNodeManagementConcurrentDuplicateAdmitsOnce(t *testing.T) {
	ctx := context.Background()
	f := newNodeManagementFixture()
	entered, release := make(chan struct{}), make(chan struct{})
	f.manage = func(r nodeplane.ManagementRequest) (api.NodeOperationReceipt, error) {
		close(entered)
		<-release
		return api.NodeOperationReceipt{Ref: r.Ref, Outcome: api.NodeCommitted}, nil
	}
	m := NewNodeManagement(f, nil)
	r := nodeIntent(t, "local", "once")
	done := make(chan api.NodeOperationReceipt, 1)
	go func() { result, _ := m.ChangeNodeConfiguration(ctx, r); done <- result }()
	<-entered
	duplicate, err := m.ChangeNodeConfiguration(ctx, r)
	if err != nil || duplicate.Outcome != api.NodeUnknown {
		close(release)
		t.Fatal("concurrent duplicate dispatched", duplicate, err)
	}
	query, err := m.ReconcileNodeOperation(ctx, r.Ref)
	if err != nil || query.Outcome != api.NodeUnknown {
		close(release)
		t.Fatal("in-flight receipt query passed before persisted intent", query, err)
	}
	close(release)
	if result := <-done; result.Outcome != api.NodeCommitted {
		t.Fatal(result)
	}
	f.mu.Lock()
	calls, recovery := f.manageCalls, f.reconcileCalls
	f.mu.Unlock()
	if calls != 1 || recovery != 0 {
		t.Fatal("concurrent native dispatch/recovery", calls, recovery)
	}
}

func TestNodeManagementRejectsReceiptAndConfigurationRetargeting(t *testing.T) {
	ctx := context.Background()
	f := newNodeManagementFixture()
	f.manage = func(r nodeplane.ManagementRequest) (api.NodeOperationReceipt, error) {
		other := r.Ref
		other.NodeID = "other"
		return api.NodeOperationReceipt{Ref: other, Outcome: api.NodeCommitted}, nil
	}
	m := NewNodeManagement(f, nil)
	r := nodeIntent(t, "local", "scoped")
	result, err := m.ChangeNodeConfiguration(ctx, r)
	if err != nil || result.Ref != r.Ref || result.Outcome != api.NodeUnknown {
		t.Fatal("foreign receipt proved original mutation", result, err)
	}
	modified := nodeIntent(t, "local", "scoped")
	modified.Change.Selection.Model = "other-model"
	modified.Ref.RequestDigest, _ = nodeplane.ManagementDigest(modified)
	if _, err := m.ChangeNodeConfiguration(ctx, modified); err == nil {
		t.Fatal("original ID changed digest")
	}
	f.configured = &api.NodeRuntimeConfiguration{Guard: api.NodeEditGuard{NodeID: "other", Backend: api.NodeCaelis, Revision: "7"}}
	if _, err := m.NodeRuntimeConfiguration(ctx, "local", api.NodeCaelis); err == nil {
		t.Fatal("foreign node configuration accepted")
	}
	if _, err := m.NodeRuntimeConfiguration(ctx, "other", api.NodeCodex); err == nil {
		t.Fatal("missing backend fell back to another runtime")
	}
}

type nodeSetupFixture struct {
	catalog                    api.NodeCatalog
	addCalls, coordinatorCalls int
}

func (f *nodeSetupFixture) Add(_ context.Context, r api.NodeAddRequest) (api.NodeAddResult, error) {
	f.addCalls++
	return api.NodeAddResult{Node: api.NodeInfo{ID: "enrolled", Label: r.Label, OS: api.NodeLinux, Join: r.Join}}, nil
}
func (f *nodeSetupFixture) Detect(_ context.Context, id string) (api.NodeInfo, error) {
	return api.NodeInfo{ID: id}, nil
}
func (f *nodeSetupFixture) JoinInstructions(_ context.Context, id string) (api.NodeJoinInstructions, error) {
	return api.NodeJoinInstructions{NodeID: id, State: api.NodeJoinWaiting, Instructions: "Complete explicit enrollment on the target machine."}, nil
}
func (f *nodeSetupFixture) SetCoordinator(_ context.Context, r api.NodeCoordinatorSelection) (api.NodeCatalog, error) {
	f.coordinatorCalls++
	c := f.catalog
	if r.NodeID != "" {
		c.Broker = &api.NodeBroker{NodeID: r.NodeID, Reachable: false}
	}
	return c, nil
}

func TestNodeSetupCASAndCoordinatorDoNotMigrateExecution(t *testing.T) {
	ctx := context.Background()
	f := newNodeManagementFixture()
	s := &nodeSetupFixture{catalog: f.catalog}
	m := NewNodeManagement(f, s)
	if _, err := m.AddNode(ctx, api.NodeAddRequest{Label: "SSH machine", Join: api.NodeSSH, SSHDestination: "user@host", ExpectedRevision: "old"}); err == nil {
		t.Fatal("stale enrollment dispatched")
	}
	if s.addCalls != 0 {
		t.Fatal("stale enrollment reached bootstrap")
	}
	if _, err := m.AddNode(ctx, api.NodeAddRequest{Label: "NAT machine", Join: api.NodeOutgoing, ExpectedRevision: "catalog-1"}); err != nil {
		t.Fatal(err)
	}
	c, err := m.SetNodeCoordinator(ctx, api.NodeCoordinatorSelection{NodeID: "other", ExpectedRevision: "catalog-1"})
	if err != nil || c.ActiveBotNodeID != "local" || c.SelectedNodeID != "local" || c.Broker == nil || c.Broker.AutomaticRoaming {
		t.Fatal("coordinator selection moved Bot or fabricated eligibility", c, err)
	}
	if _, err := m.SetNodeCoordinator(ctx, api.NodeCoordinatorSelection{NodeID: "unknown", ExpectedRevision: "catalog-1"}); err == nil {
		t.Fatal("unknown coordinator accepted")
	}
	if s.coordinatorCalls != 1 {
		t.Fatal("invalid coordinator reached native setup")
	}
}
