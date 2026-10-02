package backend

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type nodeScopedEngine struct {
	executionFake
	worker api.WorkExecutionSettings
}

func (e *nodeScopedEngine) ChangeWorkExecution(_ context.Context, v api.WorkExecutionSettings, persist func() error) error {
	if err := persist(); err != nil {
		return err
	}
	e.worker = v
	return nil
}

func TestNodeScopedPreferenceCASRetainsPermissionsAndOtherScope(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	engine := &nodeScopedEngine{}
	s := NewService(engine, nil, nil, nil, nil)
	s.ConfigureRuntime(filepath.Join(dir, "runtime.json"), api.RuntimeSettings{Runtime: "codex"})
	s.ConfigureExecution(filepath.Join(dir, "execution.json"), api.ExecutionSettings{Model: "conversation", Effort: "high", ApprovalMode: "auto", ServiceTier: "priority"})
	s.ConfigureWorkExecution(filepath.Join(dir, "worker.json"), api.WorkExecutionSettings{Model: "worker", Effort: "medium"})
	rev, _, worker, err := s.NodeExecutionScopes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r := api.NodeManagementRequest{Guard: api.NodeEditGuard{NodeID: "local", Backend: api.NodeCodex, Revision: rev}, Ref: api.NodeOperationRef{NodeID: "local", Backend: api.NodeCodex, OperationID: "original"}, Change: &api.RuntimeConfigurationChange{Action: "conversation-model", ExpectedRevision: rev, Selection: api.WorkExecutionSettings{Model: "new-conversation", Effort: "low"}}}
	result, err := s.ChangeNodeExecutionScopes(ctx, r)
	if err != nil || result.Outcome != "committed" {
		t.Fatal(result, err)
	}
	actual, err := s.ExecutionSettings()
	if err != nil || actual.Model != "new-conversation" || actual.ApprovalMode != "auto" || actual.ServiceTier != "" || s.WorkExecutionSettings() != worker {
		t.Fatal("scoped edit changed policy or worker", actual, err)
	}
	result, err = s.ChangeNodeExecutionScopes(ctx, r)
	if err != nil || result.Outcome != "conflicted" {
		t.Fatal("stale local revision admitted", result, err)
	}
	newRev, _, _, err := s.NodeExecutionScopes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r.Guard.Revision = newRev
	r.Change.ExpectedRevision = newRev
	r.Change.Selection.ServiceTier = "unsupported-expanded-tier"
	result, err = s.ChangeNodeExecutionScopes(ctx, r)
	if err != nil || result.Outcome != "rejected" {
		t.Fatal("scoped edit expanded tier capability", result, err)
	}
}

func (e *nodeScopedEngine) Models(context.Context) ([]api.ModelOption, error) {
	return []api.ModelOption{{Model: "new-conversation", ServiceTiers: []api.ServiceTier{{ID: "priority"}, {ID: "fast"}}}, {Model: "worker", ServiceTiers: []api.ServiceTier{{ID: "fast"}}}}, nil
}
func TestNodeScopedServiceTierRoundTrip(t *testing.T) {
	s := NewService(&nodeScopedEngine{}, nil, nil, nil, nil)
	s.ConfigureRuntime("", api.RuntimeSettings{Runtime: "codex"})
	s.ConfigureExecution("", api.ExecutionSettings{Model: "new-conversation", ApprovalMode: "auto"})
	for _, scope := range []string{"conversation-model", "worker-model"} {
		for _, tier := range []string{"fast", ""} {
			rev, _, _, err := s.NodeExecutionScopes(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			r := api.NodeManagementRequest{Ref: api.NodeOperationRef{NodeID: "local", Backend: api.NodeCodex}, Guard: api.NodeEditGuard{Revision: rev}, Change: &api.RuntimeConfigurationChange{Action: scope, ExpectedRevision: rev, Selection: api.WorkExecutionSettings{Model: "worker", ServiceTier: tier}}}
			result, err := s.ChangeNodeExecutionScopes(t.Context(), r)
			if err != nil || result.Outcome != "committed" {
				t.Fatal(result, err)
			}
			if scope == "conversation-model" {
				v, e := s.ExecutionSettings()
				if e != nil || v.ServiceTier != tier || v.ApprovalMode != "auto" {
					t.Fatal(v, e)
				}
			} else if v := s.WorkExecutionSettings(); v.ServiceTier != tier {
				t.Fatal(v)
			}
		}
	}
}
