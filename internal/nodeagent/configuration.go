package nodeagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

type NativeConfiguration interface {
	Read(context.Context) (api.RuntimeConfiguration, error)
	Change(context.Context, nodeplane.ManagementRequest) (api.RuntimeMutationResult, error)
}
type ConfigurationReconciler interface {
	Reconcile(context.Context, api.NodeOperationRef) (api.NodeOperationReceipt, error)
}

// ExecutionPreferences are nonsecret target-owned defaults consumed by an
// assembled owned Bot/Worker. They never modify a user's global Codex config.
type ExecutionPreferences struct {
	Schema       int                          `json:"schema"`
	Revision     uint64                       `json:"revision"`
	Conversation api.WorkExecutionSettings    `json:"conversation"`
	Worker       api.WorkExecutionSettings    `json:"worker"`
	Receipts     map[string]preferenceReceipt `json:"receipts"`
}
type preferenceReceipt struct {
	Ref    api.NodeOperationRef      `json:"ref"`
	Result api.RuntimeMutationResult `json:"result"`
}

func ReadExecutionPreferences(directory string) (ExecutionPreferences, error) {
	var p ExecutionPreferences
	err := readPrivateJSON(filepath.Join(directory, "execution.json"), &p)
	if errors.Is(err, os.ErrNotExist) {
		return ExecutionPreferences{Schema: 1, Revision: 1, Receipts: map[string]preferenceReceipt{}}, nil
	}
	if err != nil {
		return p, err
	}
	if p.Schema != 1 || p.Revision == 0 || len(p.Receipts) > 4096 {
		return p, errors.New("native execution preferences invalid")
	}
	if p.Receipts == nil {
		p.Receipts = map[string]preferenceReceipt{}
	}
	return p, nil
}

type CodexConfiguration struct {
	Directory, Binary string
	mu                sync.Mutex
}

func (c *CodexConfiguration) models(ctx context.Context) ([]api.ModelOption, error) {
	setup := &codex.Setup{}
	defer setup.Close()
	return setup.Models(ctx, c.Binary)
}
func (c *CodexConfiguration) Read(ctx context.Context) (api.RuntimeConfiguration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, err := ReadExecutionPreferences(c.Directory)
	if err != nil {
		return api.RuntimeConfiguration{}, err
	}
	models, err := c.models(ctx)
	if err != nil {
		return api.RuntimeConfiguration{}, err
	}
	revision := strconv.FormatUint(p.Revision, 10)
	return api.RuntimeConfiguration{Revision: revision, Main: p.Conversation, Models: models, Team: api.RuntimeTeam{Available: true, Revision: revision, Models: models, Roles: []api.RuntimeRole{{ID: "worker", Selection: p.Worker}}}}, nil
}
func validateSelection(v api.WorkExecutionSettings, models []api.ModelOption) error {
	if err := api.ValidateExecutionSettings(api.ExecutionSettings{Model: v.Model, Effort: v.Effort, ServiceTier: v.ServiceTier}); err != nil {
		return err
	}
	if v.Model == "" {
		return nil
	}
	for _, m := range models {
		if m.Model != v.Model {
			continue
		}
		if v.Effort != "" && !slices.Contains(m.Efforts, v.Effort) {
			return errors.New("native reasoning effort unavailable")
		}
		if v.ServiceTier != "" {
			found := false
			for _, tier := range m.ServiceTiers {
				found = found || tier.ID == v.ServiceTier
			}
			if !found {
				return errors.New("native service tier unavailable")
			}
		}
		return nil
	}
	return errors.New("native model unavailable")
}
func (c *CodexConfiguration) Change(ctx context.Context, r nodeplane.ManagementRequest) (api.RuntimeMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := api.RuntimeMutationResult{OperationID: r.Ref.OperationID, Outcome: "rejected"}
	if r.Change == nil || (r.Change.Action != "conversation-model" && r.Change.Action != "worker-model") {
		return result, nil
	}
	p, err := ReadExecutionPreferences(c.Directory)
	if err != nil {
		return result, err
	}
	if old, ok := p.Receipts[r.Ref.OperationID]; ok {
		if old.Ref != r.Ref {
			return result, nil
		}
		return old.Result, nil
	}
	if strconv.FormatUint(p.Revision, 10) != r.Change.ExpectedRevision {
		result.Outcome = "conflicted"
		return result, nil
	}
	models, err := c.models(ctx)
	if err != nil {
		return result, err
	}
	if err := validateSelection(r.Change.Selection, models); err != nil {
		return result, nil
	}
	if len(p.Receipts) >= 4096 {
		return result, errors.New("native execution receipt limit")
	}
	if r.Change.Action == "conversation-model" {
		p.Conversation = r.Change.Selection
	} else {
		p.Worker = r.Change.Selection
	}
	p.Revision++
	result.Outcome = "committed"
	p.Receipts[r.Ref.OperationID] = preferenceReceipt{Ref: r.Ref, Result: result}
	if err := localstate.Write(filepath.Join(c.Directory, "execution.json"), p); err != nil {
		result.Outcome = "unknown"
		return result, err
	}
	return result, nil
}
func (c *CodexConfiguration) Reconcile(ctx context.Context, ref api.NodeOperationRef) (api.NodeOperationReceipt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := api.NodeOperationReceipt{Ref: ref, Outcome: api.NodeUnknown, Message: "original-receipt-unavailable"}
	p, err := ReadExecutionPreferences(c.Directory)
	if err != nil {
		return r, err
	}
	if old, ok := p.Receipts[ref.OperationID]; ok && old.Ref == ref {
		r.Outcome = api.NodeOperationOutcome(old.Result.Outcome)
		r.Revision = strconv.FormatUint(p.Revision, 10)
		r.Message = ""
	}
	return r, nil
}
func (c *CodexConfiguration) Health(ctx context.Context) (NativeHealth, error) {
	setup := &codex.Setup{}
	defer setup.Close()
	state, err := setup.Inspect(ctx, c.Binary)
	if err != nil {
		return NativeHealth{}, err
	}
	h := NativeHealth{HealthKnown: true, Healthy: state.State == "ready" || state.State == "login", AuthenticationKnown: state.State == "ready" || state.State == "login", Authenticated: state.State == "ready"}
	return h, nil
}

type CaelisConfiguration struct{ Settings api.RuntimeSettings }

func (c *CaelisConfiguration) Read(ctx context.Context) (api.RuntimeConfiguration, error) {
	return caelis.ReadRuntimeConfiguration(ctx, c.Settings)
}
func (c *CaelisConfiguration) Change(ctx context.Context, r nodeplane.ManagementRequest) (api.RuntimeMutationResult, error) {
	if r.Change == nil {
		return api.RuntimeMutationResult{OperationID: r.Ref.OperationID, Outcome: "rejected"}, nil
	}
	return caelis.ChangeRuntimeConfigurationOperation(ctx, c.Settings, *r.Change, r.Ref.OperationID)
}
func (c *CaelisConfiguration) Health(ctx context.Context) (NativeHealth, error) {
	state, err := caelis.InspectSetup(ctx, c.Settings)
	if err != nil {
		return NativeHealth{}, err
	}
	return NativeHealth{HealthKnown: state.ServiceState != "unknown", Healthy: state.ServiceState == "running", AuthenticationKnown: state.State == "ready" || state.State == "models", Authenticated: state.State == "ready", SharedHost: true}, nil
}

func (c *CodexConfiguration) ExecutionScopes(ctx context.Context) (api.WorkExecutionSettings, api.WorkExecutionSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, err := ReadExecutionPreferences(c.Directory)
	return p.Conversation, p.Worker, err
}
