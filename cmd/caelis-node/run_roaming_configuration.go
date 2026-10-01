package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodecoord"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
)

// The running native executable owns the watchdog command. A renderer or agent
// request cannot replace it with an arbitrary helper executable.
func verifiedRoamingExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(executable)
	if err != nil || !filepath.IsAbs(canonical) {
		return "", errors.New("native watchdog host unavailable")
	}
	original, err := os.Stat(executable)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(canonical)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || !os.SameFile(original, info) {
		return "", errors.New("native watchdog host identity changed")
	}
	return canonical, nil
}

// The private agent journal records the original request before Change. Active
// changes target the actual owned app; default preferences only serve standby.
// An unresolved dispatch is never reconstructed or resent through another app.
type roamingActiveConfiguration struct {
	holder   *roamingProofOwner
	fallback nodeagent.NativeConfiguration
}

func (c *roamingActiveConfiguration) Read(ctx context.Context) (api.RuntimeConfiguration, error) {
	c.holder.mu.RLock()
	defer c.holder.mu.RUnlock()
	native := c.holder.native
	if native == nil {
		return c.fallback.Read(ctx)
	}
	if _, err := native.ReadRuntimeProof(ctx, c.holder.target); err != nil {
		return api.RuntimeConfiguration{}, err
	}
	if c.holder.target.Backend == "caelis" {
		return caelis.ReadRuntimeConfiguration(ctx, native.Backend.RuntimeSettings())
	}
	state, models, err := native.Backend.ReadModelSettings(ctx)
	if err != nil {
		return api.RuntimeConfiguration{}, err
	}
	revision := productmanagement.ExecutionRevision(state)
	out := api.RuntimeConfiguration{Revision: revision, Main: api.WorkExecutionSettings{Model: state.Conversation.Model, Effort: state.Conversation.Effort, ServiceTier: state.Conversation.ServiceTier}, Models: models}
	if state.Work != nil {
		out.Team = api.RuntimeTeam{Available: true, Revision: revision, Models: models, Roles: []api.RuntimeRole{{ID: "worker", Selection: *state.Work}}}
	}
	return out, nil
}
func (c *roamingActiveConfiguration) ExecutionScopes(ctx context.Context) (api.WorkExecutionSettings, api.WorkExecutionSettings, error) {
	c.holder.mu.RLock()
	defer c.holder.mu.RUnlock()
	if native := c.holder.native; native != nil {
		state, _, err := native.Backend.ReadModelSettings(ctx)
		if err != nil {
			return api.WorkExecutionSettings{}, api.WorkExecutionSettings{}, err
		}
		main := api.WorkExecutionSettings{Model: state.Conversation.Model, Effort: state.Conversation.Effort, ServiceTier: state.Conversation.ServiceTier}
		if state.Work == nil {
			return main, api.WorkExecutionSettings{}, nil
		}
		return main, *state.Work, nil
	}
	if port, ok := c.fallback.(interface {
		ExecutionScopes(context.Context) (api.WorkExecutionSettings, api.WorkExecutionSettings, error)
	}); ok {
		return port.ExecutionScopes(ctx)
	}
	return api.WorkExecutionSettings{}, api.WorkExecutionSettings{}, nodecoord.ErrIneligible
}
func (c *roamingActiveConfiguration) Change(ctx context.Context, request nodeplane.ManagementRequest) (api.RuntimeMutationResult, error) {
	c.holder.mu.RLock()
	defer c.holder.mu.RUnlock()
	result := api.RuntimeMutationResult{OperationID: request.Ref.OperationID, Outcome: "rejected"}
	native := c.holder.native
	if native == nil {
		return c.fallback.Change(ctx, request)
	}
	if request.Change == nil || request.Ref.NodeID != c.holder.target.NodeID || string(request.Ref.Backend) != c.holder.target.Backend {
		return result, nil
	}
	if _, err := native.ReadRuntimeProof(ctx, c.holder.target); err != nil {
		return result, nil
	}
	if c.holder.target.Backend == "caelis" {
		return caelis.ChangeRuntimeConfigurationOperation(ctx, native.Backend.RuntimeSettings(), *request.Change, request.Ref.OperationID)
	}
	target := ""
	switch request.Change.Action {
	case "conversation-model":
		target = "conversation"
	case "worker-model":
		target = "work"
	default:
		return result, nil
	}
	err := native.Backend.ApplyRuntimeExecutionSelection(ctx, request.Change.ExpectedRevision, target, request.Change.Selection)
	switch {
	case err == nil:
		// Keep target-owned defaults for a later fresh generation, only after the
		// actual active engine has accepted/persisted the selection.
		if defaults, ok := c.fallback.(*nodeagent.CodexConfiguration); ok {
			preferences, saveErr := nodeagent.ReadExecutionPreferences(defaults.Directory)
			if saveErr == nil {
				if target == "conversation" {
					preferences.Conversation = request.Change.Selection
				} else {
					preferences.Worker = request.Change.Selection
				}
				preferences.Revision++
				saveErr = localstate.Write(filepath.Join(defaults.Directory, "execution.json"), preferences)
			}
			if saveErr != nil {
				result.Outcome = "unknown"
				return result, saveErr
			}
		}
		result.Outcome = "committed"
	case errors.Is(err, productmanagement.ErrExecutionConflict):
		result.Outcome = "conflicted"
	case errors.Is(err, productmanagement.ErrExecutionInvalid), errors.Is(err, productmanagement.ErrExecutionUnavailable), errors.Is(err, productmanagement.ErrExecutionCancelled):
	default:
		result.Outcome = "unknown"
		return result, err
	}
	return result, nil
}
func (c *roamingActiveConfiguration) Reconcile(ctx context.Context, ref api.NodeOperationRef) (api.NodeOperationReceipt, error) {
	// The enclosing private agent owns completed receipts. Its unfinished intent
	// remains unknown even after a native generation disappears or is replaced.
	if fallback, ok := c.fallback.(nodeagent.ConfigurationReconciler); ok {
		return fallback.Reconcile(ctx, ref)
	}
	return api.NodeOperationReceipt{Ref: ref, Outcome: api.NodeUnknown, Message: "original-operation-unresolved"}, ctx.Err()
}
