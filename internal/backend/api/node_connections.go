package api

import "context"

// NodeRuntimeConnectionRef pins one explicit settings interaction to its original
// enrolled target. It contains no Store, executable, endpoint or credential.
// Opening a Runtime tab never creates this interaction; Begin is a user action.
type NodeRuntimeConnectionRef struct {
	NodeID      string      `json:"nodeId"`
	Backend     NodeBackend `json:"backend"`
	OperationID string      `json:"operationId"`
}

// NodeRuntimeConnectionController is an optional user settings port. Credentials
// are transient inputs to the target's native SDK and never receipt/journal data.
// Catalog/read operations do not start Hosts. Begin may own a bounded private
// setup Host; Close succeeds only after confirmed cleanup of that owned Host.
type NodeRuntimeConnectionController interface {
	BeginNodeRuntimeConnection(context.Context, NodeEditGuard, string) (NodeRuntimeConnectionRef, error)
	NodeRuntimeConnectionCatalog(context.Context, NodeRuntimeConnectionRef, string) (RuntimeConnectionCatalog, error)
	NodeRuntimeSetupCatalog(context.Context, NodeRuntimeConnectionRef, string, string, string) ([]SetupChoice, error)
	StartNodeRuntimeConnection(context.Context, NodeRuntimeConnectionRef, RuntimeConnectionInput) (RuntimeFlow, error)
	AdvanceNodeRuntimeConnection(context.Context, NodeRuntimeConnectionRef, RuntimeFlowAction) (RuntimeFlow, error)
	WaitNodeRuntimeConnection(context.Context, NodeRuntimeConnectionRef, string, int) (RuntimeFlow, error)
	CancelNodeRuntimeConnection(context.Context, NodeRuntimeConnectionRef, string) error
	CloseNodeRuntimeConnection(context.Context, NodeRuntimeConnectionRef) error
}
