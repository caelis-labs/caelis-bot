package api

import (
	"context"
	"errors"
)

// ErrStopNotDispatched proves rejection before any native stop was issued.
// Transport loss or an attempted stop must never use this classification.
var ErrStopNotDispatched = errors.New("source stop was not dispatched")

// ExecutionAdmission is host-installed execution authority. Begin derives a
// cancellable context for a single operation; Check is repeated after queueing
// immediately before dispatch. A nil port preserves ordinary local execution.
type ExecutionAdmission interface {
	Begin(context.Context) (context.Context, func(), error)
	CheckContext(context.Context) error
}

func BeginExecution(ctx context.Context, port ExecutionAdmission) (context.Context, func(), error) {
	if port == nil {
		return ctx, func() {}, nil
	}
	return port.Begin(ctx)
}

// WorkTargetAdmission prevents a leased owner from granting execution to a
// worker whose native lifetime does not participate in the same fence.
type WorkTargetAdmission interface {
	CheckWorkTarget(context.Context, WorkTarget) error
}

// WorkRuntimeAdmission checks the actual resolved native adapter capability.
// Catalog labels and renderer-supplied target metadata cannot enable it.
type WorkRuntimeAdmission interface {
	CheckWorkRuntime(context.Context, WorkTarget, WorkRuntime) error
}
