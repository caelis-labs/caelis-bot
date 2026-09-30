package api

import "context"

// NodeCatalog is read-only management presentation. Selection never changes
// the active Bot owner or the exact Worker target. Credentials and machine paths
// are deliberately absent. Revisions, epochs and versions are opaque strings.
type NodeCatalog struct {
	Revision        string      `json:"revision"`
	Nodes           []NodeInfo  `json:"nodes"`
	SelectedNodeID  string      `json:"selectedNodeId"`
	ActiveBotNodeID string      `json:"activeBotNodeId"`
	WorkerTarget    *WorkTarget `json:"workerTarget"`
	Broker          *NodeBroker `json:"broker"`
}

type NodeInfo struct {
	ID       string        `json:"id"`
	Label    string        `json:"label"`
	OS       NodeOS        `json:"os"`
	Join     NodeJoin      `json:"join"`
	Runtimes []NodeRuntime `json:"runtimes"`
}

type NodeOS string

const (
	NodeDarwin  NodeOS = "darwin"
	NodeLinux   NodeOS = "linux"
	NodeWindows NodeOS = "windows"
)

type NodeJoin string

const (
	NodeLocal    NodeJoin = "local"
	NodeSSH      NodeJoin = "ssh"
	NodeOutgoing NodeJoin = "outgoing"
)

type NodeBackend string

const (
	NodeCodex  NodeBackend = "codex"
	NodeCaelis NodeBackend = "caelis"
)

type NodeAuthentication string

const (
	NodeAuthUnknown   NodeAuthentication = "unknown"
	NodeAuthRequired  NodeAuthentication = "required"
	NodeAuthenticated NodeAuthentication = "authenticated"
)

type NodeHealth string

const (
	NodeHealthUnknown NodeHealth = "unknown"
	NodeHealthy       NodeHealth = "healthy"
	NodeUnavailable   NodeHealth = "unavailable"
	NodeMissing       NodeHealth = "missing"
)

// Eligibility is native negotiated role capability, not inferred from install,
// authentication or a reachable broker. Windows Codex remains Worker-only.
type NodeRuntime struct {
	Backend        NodeBackend          `json:"backend"`
	Version        string               `json:"version"`
	Authentication NodeAuthentication   `json:"authentication"`
	Health         NodeHealth           `json:"health"`
	Roles          []NodeRoleCapability `json:"roles"`
}

type NodeRoleCapability struct {
	Role     WorkRole `json:"role"`
	Eligible bool     `json:"eligible"`
	Reason   string   `json:"reason"`
}

// The designated single-user broker is optional. It is a single point of
// availability; automatic roaming is available only with an eligible node and
// a reachable broker. Its absence preserves direct in-process local operation.
type NodeBroker struct {
	NodeID           string `json:"nodeId"`
	Reachable        bool   `json:"reachable"`
	AutomaticRoaming bool   `json:"automaticRoaming"`
	Reason           string `json:"reason"`
}

// NodeEditGuard is captured when editing starts. Async completion must match
// this exact node, backend and revision before applying to the displayed state.
type NodeEditGuard struct {
	NodeID   string      `json:"nodeId"`
	Backend  NodeBackend `json:"backend"`
	Revision string      `json:"revision"`
}

// NodeOperationRef identifies the original per-node mutation. Unknown outcomes
// reconcile this reference; they never resend or move to the selected node.
type NodeOperationRef struct {
	NodeID        string      `json:"nodeId"`
	Backend       NodeBackend `json:"backend"`
	OperationID   string      `json:"operationId"`
	RequestDigest string      `json:"requestDigest"`
}

type NodeOperationOutcome string

const (
	NodeCommitted  NodeOperationOutcome = "committed"
	NodeRejected   NodeOperationOutcome = "rejected"
	NodeConflicted NodeOperationOutcome = "conflicted"
	NodeUnknown    NodeOperationOutcome = "unknown"
)

type NodeOperationReceipt struct {
	Ref      NodeOperationRef     `json:"ref"`
	Outcome  NodeOperationOutcome `json:"outcome"`
	Revision string               `json:"revision"`
	Message  string               `json:"message"`
}

type NodeInstallationAction string

const (
	NodeInstall     NodeInstallationAction = "install"
	NodeUpdate      NodeInstallationAction = "update"
	NodeCheckUpdate NodeInstallationAction = "check-update"
	NodeDetect      NodeInstallationAction = "detect"
)

type NodeInstallationChange struct {
	Action          NodeInstallationAction `json:"action"`
	Version         string                 `json:"version"`
	ExpectedVersion string                 `json:"expectedVersion"`
}

// Exactly one semantic payload is allowed; there is no arbitrary RPC or command.
type NodeManagementRequest struct {
	Guard        NodeEditGuard               `json:"guard"`
	Ref          NodeOperationRef            `json:"ref"`
	Change       *RuntimeConfigurationChange `json:"change"`
	Installation *NodeInstallationChange     `json:"installation"`
}

type NodeRuntimeConfiguration struct {
	Guard         NodeEditGuard        `json:"guard"`
	Configuration RuntimeConfiguration `json:"configuration"`
	// Codex conversation and Worker preferences are separate scopes. A null
	// scope is unavailable; never infer it from a shared Runtime main model.
	Conversation *WorkExecutionSettings `json:"conversation"`
	Worker       *WorkExecutionSettings `json:"worker"`
}

// Enrollment accepts a user-selected SSH destination, not a filesystem path,
// key, socket or runtime command. Outgoing enrollment uses native instructions.
type NodeAddRequest struct {
	Label            string   `json:"label"`
	Join             NodeJoin `json:"join"`
	SSHDestination   string   `json:"sshDestination"`
	ExpectedRevision string   `json:"expectedRevision"`
}

type NodeJoinState string

const (
	NodeJoinWaiting     NodeJoinState = "waiting"
	NodeJoinConnected   NodeJoinState = "connected"
	NodeJoinUnavailable NodeJoinState = "unavailable"
)

type NodeJoinInstructions struct {
	NodeID       string        `json:"nodeId"`
	State        NodeJoinState `json:"state"`
	Instructions string        `json:"instructions"`
}

type NodeAddResult struct {
	Node             NodeInfo              `json:"node"`
	JoinInstructions *NodeJoinInstructions `json:"joinInstructions"`
}

type NodeCoordinatorSelection struct {
	NodeID           string `json:"nodeId"`
	ExpectedRevision string `json:"expectedRevision"`
}

// These methods are explicit user management actions, never model tools.
// SelectNode changes presentation only and returns the new view revision.
type NodeManagementController interface {
	NodeCatalog(context.Context) (NodeCatalog, error)
	NodeRuntimeConfiguration(context.Context, string, NodeBackend) (NodeRuntimeConfiguration, error)
	SelectNode(context.Context, string, string) (NodeCatalog, error)
	ChangeNodeConfiguration(context.Context, NodeManagementRequest) (NodeOperationReceipt, error)
	ReconcileNodeOperation(context.Context, NodeOperationRef) (NodeOperationReceipt, error)
	AddNode(context.Context, NodeAddRequest) (NodeAddResult, error)
	DetectNode(context.Context, string) (NodeInfo, error)
	NodeJoinInstructions(context.Context, string) (NodeJoinInstructions, error)
	SetNodeCoordinator(context.Context, NodeCoordinatorSelection) (NodeCatalog, error)
}
