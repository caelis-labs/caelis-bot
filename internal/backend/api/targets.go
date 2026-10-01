package api

import (
	"context"
	"errors"
)

// LocalNodeID names this application's machine. It is independent of the
// backend's Host/Store/instance identity and preserves pre-node task bindings.
const LocalNodeID = "local"

type WorkRole string

const (
	RoleBot    WorkRole = "bot"
	RoleWorker WorkRole = "worker"
)

// WorkTarget is execution identity, not a transport address or credential.
// Backend names the native driver (codex/caelis), Role its negotiated purpose.
type WorkTarget struct {
	NodeID  string   `json:"nodeId"`
	Backend string   `json:"backend"`
	Role    WorkRole `json:"role"`
}

func (t WorkTarget) Validate() error {
	if t.NodeID == "" || t.Backend == "" || (t.Role != RoleBot && t.Role != RoleWorker) {
		return errors.New("execution target requires node, backend and role")
	}
	return nil
}

// WorkRoute contains a native host port; it must never be serialized to a UI or
// model. A registered route is ready only after native capability negotiation.
type WorkRoute struct {
	Target  WorkTarget
	Runtime WorkRuntime
}

// WorkRouter resolves only the explicitly selected target. Missing targets use
// the existing local Worker; unavailable targets must fail without fallback.
type WorkRouter interface {
	ResolveWorkTarget(*WorkTarget) (WorkTarget, error)
	WorkRuntimeFor(WorkTarget) (WorkRuntime, error)
	WorkRoutes() []WorkRoute
}

// WorkDispatchSource is attested by the resident driver's native invocation,
// never by model arguments, generated prose, task output or renderer input.
// BindingID names its exact resident Thread/Session; OperationID names the
// activating user request or existing authorized background occurrence.
type WorkDispatchSource struct {
	NodeID, Backend, BindingID, OperationID, Kind string
	Lease                                         WorkerLeaseGrant `json:"lease,omitzero"`
}

func (s WorkDispatchSource) Validate() error {
	if s.NodeID == "" || s.Backend == "" || s.BindingID == "" || s.OperationID == "" || (s.Kind != "user" && s.Kind != "authorized_background" && s.Kind != "native_activation") {
		return errors.New("work requires an attested resident request source")
	}
	if err := s.Lease.Validate(); err != nil {
		return err
	}
	if s.Lease != (WorkerLeaseGrant{}) && (s.Lease.SourceNodeID != s.NodeID || s.Lease.Backend != s.Backend) {
		return errors.New("worker lease source differs from native invocation")
	}
	return nil
}

// ErrWorkSourceInactive denotes a connected resident with no current activation.
// It does not hide transport, authority, or source-provider failures.
var ErrWorkSourceInactive = errors.New("resident work source is inactive")

type WorkSourceProvider interface {
	WorkDispatchSource(context.Context) (WorkDispatchSource, error)
}

// WorkWorkspaceProvider resolves paths on the target machine. Resolve is
// read-only; Prepare revalidates/allocates the exact directory only after the
// coordinator has durably recorded intent. Neither method dispatches a turn.
type WorkWorkspaceProvider interface {
	ResolveWorkWorkspace(context.Context, string, string) (string, error)
	PrepareWorkWorkspace(context.Context, string, string, bool) error
}

// WorkApproval retains the exact task and machine/backend/role binding of a
// native choice. A service may expose an opaque product handle, but must restore
// the original native Decision.ID and this binding before resolving it.
type WorkApproval struct {
	TaskID   string
	Target   WorkTarget
	Approval Approval
}

type WorkApprovalProvider interface {
	WorkApprovals() []WorkApproval
	DecideWork(context.Context, WorkApproval, Decision) error
}

// WorkTargetInfo is safe discovery metadata. State reports negotiated
// readiness; labels and candidate declarations never grant execution authority.
type WorkTargetInfo struct {
	Target WorkTarget `json:"target"`
	Label  string     `json:"label"`
	State  string     `json:"state"`
}

type WorkTargetCatalog interface{ WorkTargets() []WorkTargetInfo }

const MaxWorkArtifactBytes = 8 * 1024 * 1024

// WorkArtifact is host-only bounded data from one owned task's projected
// artifact ID. The service owns any local destination, never the model/adapter.
type WorkArtifact struct {
	ID, Name, MediaType, SHA256 string
	Size                        int64
	Bytes                       []byte `json:"-"`
}

type WorkArtifactProvider interface {
	ReadWorkArtifact(context.Context, string, string) (WorkArtifact, error)
}

// WorkArtifactCatalog lists canonical native projections, never identifiers
// extracted from assistant prose. References remain owned by the exact task.
type WorkArtifactRef struct {
	TaskID   string
	Target   WorkTarget
	Artifact Artifact
}

type WorkArtifactCatalog interface{ WorkArtifacts() []WorkArtifactRef }
