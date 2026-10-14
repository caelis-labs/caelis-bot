package caelis

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (s *Session) ExecutionOptions() api.ExecutionOptions {
	out := api.ExecutionOptions{DefaultApprovalMode: "auto", ApprovalModes: []api.ApprovalMode{
		{ID: "auto", Name: "自动审查", Description: "新对话保留工作区沙箱，由 Guardian 审查额外访问。"},
	}}
	if s.checkFutureBotMode("full-access") == nil {
		out.ApprovalModes = append(out.ApprovalModes, api.ApprovalMode{ID: "full-access", Name: "完全访问", Description: "只用于新对话；原生命令无工作区沙箱，已获产品授权的执行免 Runtime 审查。", Dangerous: true})
	}
	return out
}
func (s *Session) checkFutureBotMode(mode string) error {
	if mode != "full-access" {
		return nil
	}
	s.mu.Lock()
	c, info, life, manual := s.client, s.info, s.state.Connection, s.requireApproval
	s.mu.Unlock()
	if manual {
		return errors.New("当前 Caelis 会话固定为人工批准，不能为新对话选择完全访问")
	}
	if c == nil || !c.schema || !slices.Contains(info.Capabilities, "application-execution-approval-never-v1") {
		return errors.New("Caelis Host 缺少完全访问创建权限合同；请更新并重启 Host")
	}
	if !value(life.AllowUnreviewedApplicationExecution) {
		return errors.New("当前 Caelis Bot 的 Host 登记未授权免执行审查；请在原生 Host 授权流程中显式重新登记，现有授权不会自动扩大")
	}
	return nil
}
func (s *Session) CheckFutureExecutionMode(mode string) error { return s.checkFutureBotMode(mode) }
func (s *Session) SetFutureExecutionMode(mode string) {
	s.step.Lock()
	defer s.step.Unlock()
	s.execution.ApprovalMode = mode
}
func (s *Session) Models(ctx context.Context) ([]api.ModelOption, error) {
	c, e := setupClient(ctx, s.settings)
	if e != nil {
		return nil, e
	}
	defer c.http.CloseIdleConnections()
	var candidates []wire.SlashArgCandidate
	e = c.json(ctx, "POST", "/completion/slash-arguments", wire.CompletionRequest{Command: pointer("model"), Limit: pointer(1000)}, &candidates, "", "")
	if e != nil {
		return nil, e
	}
	return modelOptions(candidates), nil
}
func (s *Session) ChangeExecution(ctx context.Context, v api.ExecutionSettings, persist func() error) error {
	if e := api.ValidateExecutionSettings(v); e != nil {
		return e
	}
	if v.ApprovalMode != "workspace-write" && v.ApprovalMode != "auto" && v.ApprovalMode != "full-access" {
		return errors.New("Caelis 无法识别此执行模式")
	}
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	sid := s.state.Session.SessionId
	s.mu.Unlock()
	current, e := s.configuration(ctx, sid)
	if e != nil {
		return e
	}
	_, e = s.updateConfiguration(ctx, sid, "settings-"+rand.Text(), string(current.Revision), map[string]any{"model": v.Model, "reasoning_effort": v.Effort, "service_tier": v.ServiceTier})
	if e != nil {
		return e
	}
	if e = persist(); e != nil {
		return errors.New("运行时已保存设置，本地偏好写入失败；请重新读取设置")
	}
	// Model changes may update the live configuration, but only the separate
	// future-mode preference can change creation-time permissions.
	v.ApprovalMode = s.execution.ApprovalMode
	s.execution = v
	return nil
}

// The Host's revision-fenced DeleteModel already removes the selected model
// from active Sessions. Read it back before clearing Bot's saved preference;
// never try to clear an application model with an invalid empty patch.
func (s *Session) ReconcileDeletedModel(ctx context.Context, deleted string, persist func() error) error {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	sid := s.state.Session.SessionId
	s.mu.Unlock()
	if sid != "" {
		current, err := s.configuration(ctx, sid)
		if err != nil {
			return err
		}
		if current.Profile.Model == deleted {
			return errors.New("Caelis 尚未确认会话模型已停止使用，请刷新核对")
		}
	}
	if err := persist(); err != nil {
		return err
	}
	s.execution.Model, s.execution.Effort, s.execution.ServiceTier = "", "", ""
	return nil
}
func (s *Session) CurrentExecutionSettings(ctx context.Context) (api.ExecutionSettings, error) {
	v, e := s.Configuration(ctx)
	if e != nil {
		return api.ExecutionSettings{}, e
	}
	s.step.Lock()
	mode := api.NormalizeNewExecutionMode(s.execution.ApprovalMode)
	s.step.Unlock()
	return api.ExecutionSettings{Model: v.Profile.Model, Effort: value(v.Profile.ReasoningEffort), ServiceTier: value(v.Profile.ServiceTier), ApprovalMode: mode}, nil
}
func (s *Session) ChangeRuntime(ctx context.Context, v api.RuntimeSettings, persist func() error) (api.RuntimeCheck, error) {
	s.step.Lock()
	defer s.step.Unlock()
	if v.Runtime != "caelis" {
		return api.RuntimeCheck{}, errors.New("不能跨运行时复用绑定")
	}
	snap := s.Snapshot()
	if snap.CanInterrupt || len(snap.Approvals) > 0 || snap.Phase == "unknown" {
		return api.RuntimeCheck{}, errors.New("请先等待当前工作完成")
	}
	if e := ProbeBinding(ctx, v, filepath.Dir(s.path)); e != nil {
		return api.RuntimeCheck{}, e
	}
	if e := persist(); e != nil {
		return api.RuntimeCheck{}, e
	}
	return api.RuntimeCheck{Saved: true, Message: "已保存，下次启动生效"}, nil
}
