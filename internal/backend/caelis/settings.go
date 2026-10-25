package caelis

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
)

func (s *Session) ExecutionOptions() api.ExecutionOptions {
	return api.ExecutionOptions{DefaultApprovalMode: s.executionMode, ApprovalModes: []api.ApprovalMode{{ID: s.executionMode, Name: "自动审查", Description: "新对话由 Guardian 自动审查额外访问；保留沙箱，现有对话使用创建时的审查配置。"}}}
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
	models := modelOptions(candidates)
	if len(candidates) > 0 && len(models) == 0 {
		return nil, &api.SetupBlockedError{Code: "worker_model_authorization_required", Message: "模型服务尚未授权，请在连接与模型中补全授权"}
	}
	return models, nil
}
func (s *Session) ChangeExecution(ctx context.Context, v api.ExecutionSettings, persist func() error) error {
	if e := api.ValidateExecutionSettings(v); e != nil {
		return e
	}
	if v.ApprovalMode != "workspace-write" {
		return errors.New("当前会话的审批策略在创建时固定；模型和推理设置可实时更改")
	}
	if e := lockwait.Lock(ctx, &s.step); e != nil {
		return e
	}
	defer s.step.Unlock()
	// Reconcile original receipts before another settings mutation. A retry
	// must not turn an unknown write into a new operation with a different ID.
	if e := s.recoverConfigurations(ctx); e != nil {
		return e
	}
	s.mu.Lock()
	sid := s.state.Session.SessionId
	s.mu.Unlock()
	current, e := s.configuration(ctx, sid)
	if e != nil {
		return e
	}
	if current.Profile.Model == v.Model && value(current.Profile.ReasoningEffort) == v.Effort && value(current.Profile.ServiceTier) == v.ServiceTier {
		if e := persist(); e != nil {
			return e
		}
		s.execution = v
		return nil
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

// ConversationExecution reads the active conversation, including inherited Host defaults.
func (s *Session) ConversationExecution(ctx context.Context) (api.WorkExecutionSettings, error) {
	v, err := s.CurrentExecutionSettings(ctx)
	work := api.WorkModel(v)
	if err != nil || work.Effort != "" || work.Model == "" {
		return work, err
	}
	models, err := s.Models(ctx)
	if err != nil {
		return work, err
	}
	return inheritedWorkEffort(work, models), nil
}

// A nil native session effort means inherit this same model's native default.
// Freeze that effective value for new local/foreign Workers, without changing
// the conversation, model choice or settings. Explicit effort remains explicit.
func inheritedWorkEffort(work api.WorkExecutionSettings, models []api.ModelOption) api.WorkExecutionSettings {
	if work.Effort == "" {
		for _, model := range models {
			if model.Model == work.Model {
				work.Effort = model.DefaultEffort
				break
			}
		}
	}
	return work
}
