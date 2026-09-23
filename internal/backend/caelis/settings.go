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

func (*Session) ExecutionOptions() api.ExecutionOptions {
	return api.ExecutionOptions{DefaultApprovalMode: "workspace-write", ApprovalModes: []api.ApprovalMode{{ID: "workspace-write", Name: "受限工作区", Description: "任务仅访问自身工作目录，网络关闭；额外操作保留 Caelis 原生审批。"}}}
}
func (s *Session) Models(ctx context.Context) ([]api.ModelOption, error) {
	s.mu.Lock()
	c := s.client
	sid := s.state.Bot.SessionId
	connected := s.connected
	s.mu.Unlock()
	if !connected {
		return nil, errors.New("请先连接 Caelis")
	}
	var candidates []wire.SlashArgCandidate
	if e := c.json(ctx, "POST", "/sessions/"+idPath(sid)+"/completion/slash-arguments", wire.CompletionRequest{Command: pointer("model"), Limit: pointer(1000), SessionId: &sid}, &candidates, "", ""); e != nil {
		return nil, e
	}
	out := []api.ModelOption{}
	for _, m := range candidates {
		if value(m.NoAuth) || m.ModelSelection == nil {
			continue
		}
		v := api.ModelOption{Model: m.Value, Name: value(m.Display), Description: value(m.Detail), Default: value(m.ModelSelection.Current), DefaultEffort: m.ModelSelection.Effort, Efforts: m.ModelSelection.Efforts, ServiceTiers: []api.ServiceTier{{ID: "", Name: "标准"}}}
		if v.Name == "" {
			v.Name = m.Value
		}
		if value(m.ModelSelection.FastSupported) {
			v.ServiceTiers = append(v.ServiceTiers, api.ServiceTier{ID: "fast", Name: "Fast"})
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *Session) ChangeExecution(ctx context.Context, v api.ExecutionSettings, persist func() error) error {
	s.step.Lock()
	defer s.step.Unlock()
	if e := api.ValidateExecutionSettings(v); e != nil {
		return e
	}
	if v.ApprovalMode != "workspace-write" || (v.ServiceTier != "" && v.ServiceTier != "default" && v.ServiceTier != "fast") {
		return errors.New("Caelis 不支持此执行策略")
	}
	if !s.Snapshot().CanSend {
		return errors.New("请等待当前操作结束并核对未知回执")
	}
	models, e := s.Models(ctx)
	if e != nil {
		return e
	}
	valid := false
	for _, m := range models {
		if m.Model == v.Model && (v.Effort == "" || slices.Contains(m.Efforts, v.Effort)) {
			for _, tier := range m.ServiceTiers {
				if tier.ID == v.ServiceTier || v.ServiceTier == "" || v.ServiceTier == "default" && tier.ID == "" {
					valid = true
				}
			}
		}
	}
	if !valid {
		return errors.New("当前 Caelis 模型不支持此组合")
	}
	var bot wire.Bot
	if e = s.client.json(ctx, "GET", s.botPath(""), nil, &bot, "", ""); e != nil {
		return e
	}
	config := bot.Config
	config.Model = &v.Model
	config.Effort = &v.Effort
	config.Fast = pointer(v.ServiceTier == "fast")
	op := "config-" + rand.Text()
	out, e := s.command(ctx, op, "/sessions/"+idPath(bot.SessionId)+"/bots/update", wire.UpdateBotRequest{BotId: bot.Id, SessionId: &bot.SessionId, ExpectedRevision: &bot.Revision, OperationId: &op, Config: config})
	if e != nil {
		return e
	}
	if !succeeded(out.Outcome) {
		return errors.New("Caelis 设置未确认，请重新连接核对")
	}
	if e = persist(); e != nil {
		return errors.New("Caelis 已接受设置，但本地偏好未能保存，请重新读取设置")
	}
	return nil
}
func (s *Session) ChangeRuntime(ctx context.Context, v api.RuntimeSettings, persist func() error) (api.RuntimeCheck, error) {
	if v.Runtime != "caelis" {
		return api.RuntimeCheck{}, errors.New("不能将 Caelis 绑定用于其他后端")
	}
	s.step.Lock()
	defer s.step.Unlock()
	snap := s.Snapshot()
	if snap.CanInterrupt || len(snap.Approvals) > 0 || snap.Phase == "unknown" {
		return api.RuntimeCheck{}, errors.New("工作未结束，暂时不能更改连接")
	}
	if e := ProbeBinding(ctx, v, filepath.Dir(s.path)); e != nil {
		return api.RuntimeCheck{}, e
	}
	if e := persist(); e != nil {
		return api.RuntimeCheck{}, e
	}
	return api.RuntimeCheck{Saved: true, Message: "已检测并保存，下次启动生效；当前连接保持不变。"}, nil
}

func (s *Session) CurrentExecutionSettings(ctx context.Context) (api.ExecutionSettings, error) {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	c, connected := s.client, s.connected
	path := s.botPath("")
	s.mu.Unlock()
	if !connected {
		return api.ExecutionSettings{}, errors.New("请先连接 Caelis")
	}
	var b wire.Bot
	if e := c.json(ctx, "GET", path, nil, &b, "", ""); e != nil {
		return api.ExecutionSettings{}, e
	}
	model := value(b.ModelSelector)
	if model == "" {
		model = value(b.Config.Model)
	}
	tier := ""
	if value(b.Config.Fast) {
		tier = "fast"
	}
	return api.ExecutionSettings{Model: model, Effort: value(b.Config.Effort), ServiceTier: tier, ApprovalMode: "workspace-write"}, nil
}
