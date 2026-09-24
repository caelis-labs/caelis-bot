package caelis

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (s *Session) ExecutionOptions() api.ExecutionOptions {
	return api.ExecutionOptions{DefaultApprovalMode: s.executionMode, ApprovalModes: []api.ApprovalMode{{ID: s.executionMode, Name: "运行时受限执行", Description: "工作区内执行；额外访问由运行时请求审批。"}}}
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
	if v.ApprovalMode != "workspace-write" {
		return errors.New("当前会话的审批策略在创建时固定；模型和推理设置可实时更改")
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
	s.execution = v
	return nil
}
func (s *Session) CurrentExecutionSettings(ctx context.Context) (api.ExecutionSettings, error) {
	v, e := s.Configuration(ctx)
	if e != nil {
		return api.ExecutionSettings{}, e
	}
	return api.ExecutionSettings{Model: v.Profile.Model, Effort: value(v.Profile.ReasoningEffort), ServiceTier: value(v.Profile.ServiceTier), ApprovalMode: "workspace-write"}, nil
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
