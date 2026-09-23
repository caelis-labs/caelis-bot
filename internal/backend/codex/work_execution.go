package codex

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Session) ChangeWorkExecution(ctx context.Context, v api.WorkExecutionSettings, persist func() error) error {
	s.op.Lock()
	defer s.op.Unlock()
	var catalog []api.ModelOption
	if v.Model != "" {
		var err error
		catalog, err = s.Models(ctx)
		if err != nil {
			return err
		}
	}
	if err := api.ValidateWorkExecution(v, catalog); err != nil {
		return err
	}
	if err := persist(); err != nil {
		return err
	}
	s.mu.Lock()
	s.opts.WorkExecution = v
	s.mu.Unlock()
	return nil
}

// Read effective Runtime configuration, not model/list's catalog default and
// not the resident Bot thread. Failed reads are not an absent model.
func (s *Session) resolveWorkExecution(ctx context.Context, workspace string) (api.WorkExecutionSettings, error) {
	s.mu.Lock()
	v, fallback, c := s.opts.WorkExecution, api.WorkModel(s.opts.Execution), s.client
	if fallback.Model == "" {
		fallback = s.residentExecution
	}
	s.mu.Unlock()
	if v.Model != "" {
		return v, nil
	}
	var response struct {
		Config *struct {
			Model       json.RawMessage `json:"model"`
			Effort      string          `json:"model_reasoning_effort"`
			ServiceTier string          `json:"service_tier"`
		} `json:"config"`
	}
	if err := callDecode(ctx, c, "config/read", map[string]any{"includeLayers": false, "cwd": workspace}, &response); err != nil || response.Config == nil {
		return v, errors.New("无法读取 Codex 默认模型；请检查连接后重试，或手动指定工作模型")
	}
	var model string
	if json.Unmarshal(response.Config.Model, &model) != nil {
		return v, errors.New("Codex 默认模型配置不完整，请重试或手动指定工作模型")
	}
	if model == "" {
		return fallback, nil
	}
	return api.WorkExecutionSettings{Model: model, Effort: response.Config.Effort, ServiceTier: response.Config.ServiceTier}, nil
}

// The native receipt pins even a built-in default when neither Runtime nor Bot
// explicitly selected a model. Legacy tasks resume without model overrides and
// acquire this receipt before the next turn.
type threadExecutionResponse struct {
	Thread          nativeThread `json:"thread"`
	Model           string       `json:"model"`
	ModelProvider   string       `json:"modelProvider"`
	ReasoningEffort string       `json:"reasoningEffort"`
	ServiceTier     string       `json:"serviceTier"`
}

func (r threadExecutionResponse) execution() *api.WorkExecutionSettings {
	return &api.WorkExecutionSettings{Model: r.Model, Effort: r.ReasoningEffort, ServiceTier: r.ServiceTier}
}

func (s *Session) applyWorkExecution(params map[string]any, thread bool, t *taskRecord) {
	v := s.opts.Execution
	v.Model, v.Effort, v.ServiceTier = "", "", ""
	if t.Execution != nil {
		v.Model, v.Effort, v.ServiceTier = t.Execution.Model, t.Execution.Effort, t.Execution.ServiceTier
	}
	s.applyExecutionSettings(params, thread, v)
	if thread && t.ModelProvider != "" {
		params["modelProvider"] = t.ModelProvider
	}
}
