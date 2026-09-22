package codex

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type modelEntry struct {
	Model                     string `json:"model"`
	DisplayName               string `json:"displayName"`
	Description               string `json:"description"`
	IsDefault                 bool   `json:"isDefault"`
	DefaultReasoningEffort    string `json:"defaultReasoningEffort"`
	SupportedReasoningEfforts []struct {
		ReasoningEffort string `json:"reasoningEffort"`
	} `json:"supportedReasoningEfforts"`
	ServiceTiers []api.ServiceTier `json:"serviceTiers"`
}

func (s *Session) Models(ctx context.Context) ([]api.ModelOption, error) {
	s.mu.Lock()
	c := s.client
	ready := s.state.Connection == "ready" && !s.closed && !s.closing
	s.mu.Unlock()
	if c == nil || !ready {
		return nil, errors.New("连接 Codex 后可加载模型；请在对话窗口检查连接")
	}
	ctx, cancel := s.operation(ctx, 15*time.Second)
	defer cancel()
	result := []api.ModelOption{}
	cursor := ""
	seen := map[string]bool{}
	models := map[string]bool{}
	for range 20 {
		params := map[string]any{"limit": 100, "includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page struct {
			Data       []modelEntry `json:"data"`
			NextCursor string       `json:"nextCursor"`
		}
		if err := callDecode(ctx, c, "model/list", params, &page); err != nil {
			return nil, errors.New("Codex 模型目录暂不可用，请刷新；不会自动换用其他模型")
		}
		for _, m := range page.Data {
			if m.Model == "" || models[m.Model] {
				continue
			}
			models[m.Model] = true
			efforts := []string{}
			for _, e := range m.SupportedReasoningEfforts {
				if e.ReasoningEffort != "" {
					efforts = append(efforts, e.ReasoningEffort)
				}
			}
			tiers := m.ServiceTiers
			if tiers == nil {
				tiers = []api.ServiceTier{}
			}
			result = append(result, api.ModelOption{Model: m.Model, Name: m.DisplayName, Description: m.Description, Default: m.IsDefault, DefaultEffort: m.DefaultReasoningEffort, Efforts: efforts, ServiceTiers: tiers})
		}
		if page.NextCursor == "" {
			return result, nil
		}
		if seen[page.NextCursor] {
			break
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
	return nil, errors.New("模型目录分页异常，请刷新")
}
func (s *Session) ChangeExecution(ctx context.Context, v api.ExecutionSettings, persist func() error) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	busy := s.run != "" || len(s.childRuns) > 0 || s.binding.Pending != nil || len(s.prompts) > 0 || s.closed || s.closing || s.state.Phase == "unknown"
	s.mu.Unlock()
	if busy {
		return errors.New("请先结束当前工作并处理待确认事项，再保存模型与权限设置")
	}
	if err := api.ValidateExecutionSettings(v); err != nil {
		return err
	}
	if v.Model == "" {
		return errors.New("请选择 Codex 目录中的模型")
	}
	catalog, err := s.Models(ctx)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(catalog, func(m api.ModelOption) bool { return m.Model == v.Model })
	if index < 0 {
		return errors.New("该模型已不在 Codex 目录中，请刷新")
	}
	model := catalog[index]
	if !slices.Contains(model.Efforts, v.Effort) && !(v.Effort == "" && len(model.Efforts) == 0) {
		return errors.New("该模型不支持所选推理强度，请重新选择")
	}
	if v.ServiceTier != "" && !slices.ContainsFunc(model.ServiceTiers, func(t api.ServiceTier) bool { return t.ID == v.ServiceTier }) {
		return errors.New("该模型不支持所选速度档位，请重新选择")
	}
	if err = persist(); err != nil {
		return err
	}
	s.mu.Lock()
	s.opts.Execution = v
	s.mu.Unlock()
	return nil
}
func (s *Session) applyExecution(params map[string]any, thread bool) {
	v := s.opts.Execution
	policy, reviewer, sandbox, kind := "on-request", "auto_review", "workspace-write", "workspaceWrite"
	switch v.ApprovalMode {
	case "ask":
		reviewer = "user"
	case "read-only":
		policy, reviewer, sandbox, kind = "never", "user", "read-only", "readOnly"
	case "full-access":
		policy, reviewer, sandbox, kind = "never", "user", "danger-full-access", "dangerFullAccess"
	}
	if s.opts.RequireApproval {
		policy, reviewer, sandbox, kind = "untrusted", "user", "workspace-write", "workspaceWrite"
	}
	params["approvalPolicy"], params["approvalsReviewer"] = policy, reviewer
	if thread {
		params["sandbox"] = sandbox
	} else {
		p := map[string]any{"type": kind}
		if kind == "workspaceWrite" {
			p["writableRoots"] = []string{s.opts.Directory}
			p["networkAccess"] = false
		}
		params["sandboxPolicy"] = p
	}
	if v.Model == "" {
		return
	} // Preserve native defaults until explicitly configured.
	params["model"] = v.Model
	// Null clears a previous Fast selection; omission would preserve it.
	params["serviceTier"] = nil
	if v.ServiceTier != "" {
		params["serviceTier"] = v.ServiceTier
	}
	if thread {
		config := params["config"].(map[string]any)
		if v.Effort != "" {
			config["model_reasoning_effort"] = v.Effort
		}
	} else {
		params["effort"] = nil
		if v.Effort != "" {
			params["effort"] = v.Effort
		}
	}
}
