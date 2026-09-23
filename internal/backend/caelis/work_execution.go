package caelis

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (s *Session) ChangeWorkExecution(ctx context.Context, v api.WorkExecutionSettings, persist func() error) error {
	s.step.Lock()
	defer s.step.Unlock()
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
	s.workExecution = v
	s.mu.Unlock()
	return nil
}

// Read only Host model metadata. Never copy credentials or change global
// configuration when creating an application-owned worker.
func (s *Session) resolveWorkExecution(ctx context.Context, fallback wire.ApplicationProfile) (api.WorkExecutionSettings, error) {
	s.mu.Lock()
	v := s.workExecution
	s.mu.Unlock()
	if v.Model != "" {
		return v, nil
	}
	c, err := setupClient(ctx, s.settings)
	if err != nil {
		return v, err
	}
	defer c.http.CloseIdleConnections()
	var candidates []wire.SlashArgCandidate
	if err = c.json(ctx, "POST", "/completion/slash-arguments", wire.CompletionRequest{Command: pointer("model"), Limit: pointer(1000)}, &candidates, "", ""); err != nil {
		return v, errors.New("无法读取 Caelis 默认模型；请检查连接后重试，或手动指定工作模型")
	}
	for _, m := range candidates {
		if m.ModelSelection == nil || !value(m.ModelSelection.Current) {
			continue
		}
		if value(m.NoAuth) || m.Value == "" {
			return v, errors.New("Caelis 默认模型尚未就绪，请在运行时配置模型或手动指定工作模型")
		}
		v = api.WorkExecutionSettings{Model: m.Value, Effort: m.ModelSelection.Effort}
		if value(m.ModelSelection.Fast) {
			v.ServiceTier = "priority"
		}
		return v, nil
	}
	// Some non-reasoning models omit ModelSelection. Only use a structured
	// status identity, never parse the human-readable model display string.
	var status wire.StatusSnapshot
	if err = c.json(ctx, "GET", "/status", nil, &status, "", ""); err != nil || status.Configuration.Revision == "" {
		return v, errors.New("无法读取 Caelis 默认模型；请检查连接后重试，或手动指定工作模型")
	}
	model := status.ModelStatus
	alias := value(model.Alias)
	if len(candidates) == 0 && alias == "" && value(model.Name) == "" && value(model.Provider) == "" {
		return api.WorkExecutionSettings{Model: fallback.Model, Effort: value(fallback.ReasoningEffort), ServiceTier: value(fallback.ServiceTier)}, nil
	}
	if alias == "" || value(model.MissingApiKey) {
		return v, errors.New("无法确定 Caelis 默认模型，请刷新运行时配置或手动指定工作模型")
	}
	for _, m := range candidates {
		if m.Value != alias && value(m.Display) != alias && value(m.ModelConfigId) != alias {
			continue
		}
		if value(m.NoAuth) || m.Value == "" {
			return v, errors.New("Caelis 默认模型尚未就绪，请在运行时配置模型或手动指定工作模型")
		}
		v = api.WorkExecutionSettings{Model: m.Value, Effort: value(model.ReasoningEffort)}
		if value(model.FastMode) {
			v.ServiceTier = "priority"
		}
		return v, nil
	}
	return v, errors.New("无法确定 Caelis 默认模型，请刷新运行时配置或手动指定工作模型")
}
