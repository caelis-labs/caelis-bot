package app

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

type nodeRuntimeKey struct {
	node    string
	backend api.NodeBackend
}
type nodeOperationKey struct {
	nodeRuntimeKey
	operation string
}
type nodeManagementOperation struct {
	ref      api.NodeOperationRef
	receipt  api.NodeOperationReceipt
	inflight bool
}

type nodeManagement struct {
	agent      nodeplane.CatalogAgent
	setup      nodeplane.NodeSetup
	mu         sync.Mutex
	selected   string
	operations map[nodeOperationKey]nodeManagementOperation
	pending    map[nodeRuntimeKey]nodeOperationKey
}

// NewNodeManagement assembles only explicit user management. The trusted agent
// owns durable intent/receipts, and setup owns native pairing/bootstrap. Neither
// port is required by the ordinary local execution path.
func NewNodeManagement(agent nodeplane.CatalogAgent, setup nodeplane.NodeSetup) api.NodeManagementController {
	return &nodeManagement{agent: agent, setup: setup, operations: map[nodeOperationKey]nodeManagementOperation{}, pending: map[nodeRuntimeKey]nodeOperationKey{}}
}

func (m *nodeManagement) catalog(ctx context.Context) (api.NodeCatalog, error) {
	if m.agent == nil {
		return api.NodeCatalog{}, errors.New("node catalog is unavailable")
	}
	c, err := m.agent.Catalog(ctx)
	if err != nil {
		return c, err
	}
	if err = nodeplane.ValidateCatalog(c); err != nil {
		return api.NodeCatalog{}, err
	}
	return c, nil
}

func (m *nodeManagement) view(c api.NodeCatalog) api.NodeCatalog {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.selected == "" {
		m.selected = c.SelectedNodeID
	}
	if m.selected == "" {
		for _, n := range c.Nodes {
			if n.ID == api.LocalNodeID {
				m.selected = n.ID
				break
			}
		}
	}
	if m.selected == "" && len(c.Nodes) > 0 {
		m.selected = c.Nodes[0].ID
	}
	c.SelectedNodeID = ""
	for _, n := range c.Nodes {
		if n.ID == m.selected {
			c.SelectedNodeID = m.selected
			break
		}
	}
	// A missing selected node leaves no selection. Never move active ownership.
	return c
}

func (m *nodeManagement) NodeCatalog(ctx context.Context) (api.NodeCatalog, error) {
	c, err := m.catalog(ctx)
	if err != nil {
		return c, err
	}
	return m.view(c), nil
}

func (m *nodeManagement) SelectNode(ctx context.Context, nodeID, revision string) (api.NodeCatalog, error) {
	c, err := m.catalog(ctx)
	if err != nil {
		return c, err
	}
	if revision == "" || revision != c.Revision {
		return api.NodeCatalog{}, errors.New("node catalog changed; refresh before selecting")
	}
	found := false
	for _, n := range c.Nodes {
		found = found || n.ID == nodeID
	}
	if !found {
		return api.NodeCatalog{}, errors.New("selected node is unavailable")
	}
	m.mu.Lock()
	m.selected = nodeID
	m.mu.Unlock()
	return m.view(c), nil
}

func (m *nodeManagement) NodeRuntimeConfiguration(ctx context.Context, nodeID string, backend api.NodeBackend) (api.NodeRuntimeConfiguration, error) {
	if m.agent == nil {
		return api.NodeRuntimeConfiguration{}, errors.New("node configuration is unavailable")
	}
	c, err := m.catalog(ctx)
	if err != nil {
		return api.NodeRuntimeConfiguration{}, err
	}
	found := false
	for _, n := range c.Nodes {
		if n.ID == nodeID {
			for _, r := range n.Runtimes {
				found = found || r.Backend == backend
			}
		}
	}
	if !found {
		return api.NodeRuntimeConfiguration{}, errors.New("exact node runtime is unavailable")
	}
	v, err := m.agent.Configuration(ctx, nodeID, backend)
	if err != nil {
		return v, err
	}
	if v.Guard.NodeID != nodeID || v.Guard.Backend != backend || v.Guard.Revision == "" {
		return api.NodeRuntimeConfiguration{}, errors.New("configuration did not retain exact node runtime")
	}
	return v, nil
}

func operationKey(ref api.NodeOperationRef) nodeOperationKey {
	return nodeOperationKey{nodeRuntimeKey: nodeRuntimeKey{node: ref.NodeID, backend: ref.Backend}, operation: ref.OperationID}
}

func unknownNodeReceipt(ref api.NodeOperationRef) api.NodeOperationReceipt {
	return api.NodeOperationReceipt{Ref: ref, Outcome: api.NodeUnknown, Message: "Operation outcome is unknown; reconcile its original receipt."}
}

func normalizeNodeReceipt(ref api.NodeOperationRef, r api.NodeOperationReceipt, err error) api.NodeOperationReceipt {
	if err != nil || r.Ref != ref {
		return unknownNodeReceipt(ref)
	}
	switch r.Outcome {
	case api.NodeCommitted, api.NodeRejected, api.NodeConflicted, api.NodeUnknown:
		return r
	default:
		return unknownNodeReceipt(ref)
	}
}

func (m *nodeManagement) finish(ref api.NodeOperationRef, r api.NodeOperationReceipt) {
	key := operationKey(ref)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.operations[key] = nodeManagementOperation{ref: ref, receipt: r}
	if r.Outcome != api.NodeUnknown {
		if pending, ok := m.pending[key.nodeRuntimeKey]; ok && pending == key {
			delete(m.pending, key.nodeRuntimeKey)
		}
	}
}

func (m *nodeManagement) ChangeNodeConfiguration(ctx context.Context, r api.NodeManagementRequest) (api.NodeOperationReceipt, error) {
	// Freeze pointer payloads before digest validation and asynchronous native IO.
	if r.Change != nil {
		c := *r.Change
		r.Change = &c
	}
	if r.Installation != nil {
		i := *r.Installation
		r.Installation = &i
	}
	if err := nodeplane.ValidateManagementRequest(r); err != nil {
		return api.NodeOperationReceipt{}, err
	}
	if m.agent == nil {
		return api.NodeOperationReceipt{}, errors.New("node management is unavailable")
	}
	// Rehydrate original references from native journals after APP/owner remount.
	// The native agent repeats this check atomically at actual dispatch.
	catalog, catalogErr := m.catalog(ctx)
	if catalogErr != nil {
		return api.NodeOperationReceipt{}, catalogErr
	}
	for _, ref := range catalog.PendingOperations {
		if ref.NodeID == r.Ref.NodeID && ref.Backend == r.Ref.Backend {
			if ref != r.Ref {
				return api.NodeOperationReceipt{}, errors.New("reconcile the original journaled operation on this node runtime first")
			}
			return unknownNodeReceipt(ref), nil
		}
	}
	key := operationKey(r.Ref)
	m.mu.Lock()
	if previous, ok := m.operations[key]; ok {
		m.mu.Unlock()
		if previous.ref != r.Ref {
			return api.NodeOperationReceipt{}, errors.New("original operation ID cannot change intent")
		}
		return previous.receipt, nil // In-flight/unknown operations are never resent.
	}
	if _, ok := m.pending[key.nodeRuntimeKey]; ok {
		m.mu.Unlock()
		return api.NodeOperationReceipt{}, errors.New("reconcile the original pending operation on this node runtime first")
	}
	m.operations[key] = nodeManagementOperation{ref: r.Ref, receipt: unknownNodeReceipt(r.Ref), inflight: true}
	m.pending[key.nodeRuntimeKey] = key
	m.mu.Unlock()
	// The agent persists this exact intent before native dispatch and performs
	// the revision CAS. A lost response stays bound to the original node runtime.
	result, err := m.agent.Manage(ctx, r)
	result = normalizeNodeReceipt(r.Ref, result, err)
	m.finish(r.Ref, result)
	return result, nil
}

func (m *nodeManagement) ReconcileNodeOperation(ctx context.Context, ref api.NodeOperationRef) (api.NodeOperationReceipt, error) {
	if err := nodeplane.ValidateOperationRef(ref); err != nil {
		return api.NodeOperationReceipt{}, err
	}
	if m.agent == nil {
		return api.NodeOperationReceipt{}, errors.New("node receipt recovery is unavailable")
	}
	key := operationKey(ref)
	m.mu.Lock()
	if previous, ok := m.operations[key]; ok {
		if previous.ref != ref {
			m.mu.Unlock()
			return api.NodeOperationReceipt{}, errors.New("receipt reference changed original intent")
		}
		if previous.inflight || previous.receipt.Outcome != api.NodeUnknown {
			m.mu.Unlock()
			return previous.receipt, nil
		}
	}
	if pending, ok := m.pending[key.nodeRuntimeKey]; ok && pending != key {
		m.mu.Unlock()
		return api.NodeOperationReceipt{}, errors.New("another original operation is pending on this node runtime")
	}
	m.operations[key] = nodeManagementOperation{ref: ref, receipt: unknownNodeReceipt(ref), inflight: true}
	m.pending[key.nodeRuntimeKey] = key
	m.mu.Unlock()
	result, err := m.agent.Reconcile(ctx, ref)
	result = normalizeNodeReceipt(ref, result, err)
	m.finish(ref, result)
	return result, nil
}

func (m *nodeManagement) AddNode(ctx context.Context, r api.NodeAddRequest) (api.NodeAddResult, error) {
	if m.setup == nil {
		return api.NodeAddResult{}, errors.New("node enrollment is unavailable")
	}
	if r.Label == "" || (r.Join != api.NodeSSH && r.Join != api.NodeOutgoing) || (r.Join == api.NodeSSH && r.SSHDestination == "") || (r.Join == api.NodeOutgoing && r.SSHDestination != "") {
		return api.NodeAddResult{}, errors.New("invalid node enrollment")
	}
	c, err := m.catalog(ctx)
	if err != nil {
		return api.NodeAddResult{}, err
	}
	if r.ExpectedRevision == "" || r.ExpectedRevision != c.Revision {
		return api.NodeAddResult{}, errors.New("node catalog changed; refresh before enrollment")
	}
	result, err := m.setup.Add(ctx, r)
	if err != nil {
		return result, err
	}
	if result.Node.ID == "" || result.Node.Join != r.Join {
		return api.NodeAddResult{}, errors.New("enrollment did not establish a verified node")
	}
	return result, nil
}

func (m *nodeManagement) DetectNode(ctx context.Context, nodeID string) (api.NodeInfo, error) {
	if m.setup == nil || nodeID == "" {
		return api.NodeInfo{}, errors.New("node detection is unavailable")
	}
	n, err := m.setup.Detect(ctx, nodeID)
	if err == nil && n.ID != nodeID {
		return api.NodeInfo{}, errors.New("detection changed node identity")
	}
	return n, err
}

func (m *nodeManagement) NodeJoinInstructions(ctx context.Context, nodeID string) (api.NodeJoinInstructions, error) {
	if m.setup == nil || nodeID == "" {
		return api.NodeJoinInstructions{}, errors.New("node join instructions are unavailable")
	}
	i, err := m.setup.JoinInstructions(ctx, nodeID)
	if err == nil && (i.NodeID != nodeID || (i.State != api.NodeJoinWaiting && i.State != api.NodeJoinConnected && i.State != api.NodeJoinUnavailable)) {
		return api.NodeJoinInstructions{}, errors.New("invalid node join instructions")
	}
	return i, err
}

func (m *nodeManagement) SetNodeCoordinator(ctx context.Context, r api.NodeCoordinatorSelection) (api.NodeCatalog, error) {
	if m.setup == nil {
		return api.NodeCatalog{}, errors.New("node coordinator selection is unavailable")
	}
	c, err := m.catalog(ctx)
	if err != nil {
		return c, err
	}
	if r.ExpectedRevision == "" || r.ExpectedRevision != c.Revision {
		return api.NodeCatalog{}, errors.New("node catalog changed; refresh before choosing coordinator")
	}
	if r.NodeID != "" {
		found := false
		for _, n := range c.Nodes {
			found = found || n.ID == r.NodeID
		}
		if !found {
			return api.NodeCatalog{}, errors.New("designated coordinator node is unavailable")
		}
	}
	v, err := m.setup.SetCoordinator(ctx, r)
	if err != nil {
		return v, err
	}
	if err = nodeplane.ValidateCatalog(v); err != nil {
		return api.NodeCatalog{}, err
	}
	if v.ActiveBotNodeID != c.ActiveBotNodeID || (v.WorkerTarget == nil) != (c.WorkerTarget == nil) || (v.WorkerTarget != nil && *v.WorkerTarget != *c.WorkerTarget) {
		return api.NodeCatalog{}, errors.New("coordinator selection cannot migrate execution")
	}
	if (r.NodeID == "" && v.Broker != nil) || (r.NodeID != "" && (v.Broker == nil || v.Broker.NodeID != r.NodeID)) {
		return api.NodeCatalog{}, errors.New("coordinator selection was not confirmed")
	}
	return m.view(v), nil
}

var _ api.NodeManagementController = (*nodeManagement)(nil)

func (m *nodeManagement) Close() error {
	if closer, ok := m.agent.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
