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
}

func (s WorkDispatchSource) Validate() error {
	if s.NodeID == "" || s.Backend == "" || s.BindingID == "" || s.OperationID == "" || (s.Kind != "user" && s.Kind != "authorized_background" && s.Kind != "native_activation") {
		return errors.New("work requires an attested resident request source")
	}
	return nil
}

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
