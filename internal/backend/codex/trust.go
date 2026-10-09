package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

// Project config is executable configuration. The resident Bot requires its
// private cwd to be trusted before thread/start or thread/resume loads it.
// Read through the connected App Server so shared and private owners agree.
type workspaceTrust struct {
	level  string // trusted, untrusted, or absent
	reason string
}

func readWorkspaceTrust(ctx context.Context, c *Client, directory string) (workspaceTrust, error) {
	if directory == "" || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return workspaceTrust{}, errors.New("Bot 工作目录不是规范的绝对路径")
	}
	var response struct {
		Config *struct {
			Projects map[string]struct {
				TrustLevel string `json:"trust_level"`
			} `json:"projects"`
		} `json:"config"`
		Layers *[]struct {
			Name struct {
				Type string `json:"type"`
			} `json:"name"`
			DisabledReason string `json:"disabledReason"`
		} `json:"layers"`
	}
	if err := callDecode(ctx, c, "config/read", map[string]any{"includeLayers": true, "cwd": directory}, &response); err != nil {
		return workspaceTrust{}, fmt.Errorf("无法读取 Codex 工作目录信任状态: %w", err)
	}
	if response.Config == nil || response.Layers == nil {
		return workspaceTrust{}, errors.New("Codex 未返回工作目录信任所需的配置层；请更新 Codex")
	}
	state := workspaceTrust{}
	// A saved exact decision wins. An explicitly untrusted ancestor is also
	// user intent and must never be silently overridden by Bot.
	exact, ancestorTrusted, ancestorUntrusted := "", false, false
	for path, project := range response.Config.Projects {
		if path != directory && !strings.HasPrefix(directory, path+string(filepath.Separator)) {
			continue
		}
		if project.TrustLevel != "trusted" && project.TrustLevel != "untrusted" {
			continue
		}
		if path == directory {
			exact = project.TrustLevel
		} else if project.TrustLevel == "trusted" {
			ancestorTrusted = true
		} else {
			ancestorUntrusted = true
		}
	}
	for _, layer := range *response.Layers {
		if layer.Name.Type == "project" && layer.DisabledReason != "" {
			state.reason = layer.DisabledReason
			break
		}
	}
	switch {
	case exact != "":
		state.level = exact
	case ancestorUntrusted:
		state.level = "untrusted"
	case ancestorTrusted && state.reason == "":
		state.level = "trusted"
	}
	if exact == "trusted" && state.reason != "" {
		return state, fmt.Errorf("Codex 工作目录配置被禁用: %s", state.reason)
	}
	return state, nil
}

func writeBotWorkspaceTrust(ctx context.Context, c *Client, directory string) error {
	key := "projects." + strconvQuoteConfigKey(directory) + ".trust_level"
	params := map[string]any{"edits": []any{map[string]any{"keyPath": key, "value": "trusted", "mergeStrategy": "replace"}}, "reloadUserConfig": true}
	if err := callDecode(ctx, c, "config/batchWrite", params, nil); err != nil {
		var native *NativeError
		if errors.As(err, &native) {
			if reason := plugins.SafeDisplayDescription(native.Message); reason != "" {
				return fmt.Errorf("Codex 未能保存 Bot 工作目录信任: %s: %w", reason, err)
			}
		}
		return fmt.Errorf("Codex 未能保存 Bot 工作目录信任: %w", err)
	}
	return nil
}

func strconvQuoteConfigKey(path string) string {
	b, _ := json.Marshal(path)
	return string(b)
}

func (s *Session) trustFailure(message string, cause error) error {
	s.mu.Lock()
	s.trustBlockedReason = message
	if s.trustApprovalID != "" {
		for i := range s.state.Approvals {
			if s.state.Approvals[i].ID == s.trustApprovalID {
				s.state.Approvals[i].Status = "unknown"
			}
		}
		s.trustApprovalID = ""
	}
	s.mu.Unlock()
	err := s.connectionError(message, cause)
	s.mu.Lock()
	s.state.ConnectionIssue = "workspace_trust"
	s.state.Message = message
	s.update()
	s.mu.Unlock()
	return err
}

func (s *Session) trustWriteFailure(err error) error {
	var request *RequestError
	if errors.As(err, &request) && request.OutcomeUnknown {
		s.mu.Lock()
		s.binding.TrustWriteUnknown = true
		persistErr := s.save()
		s.mu.Unlock()
		if persistErr != nil {
			err = errors.Join(err, fmt.Errorf("无法持久保存未知信任写入回执: %w", persistErr))
		}
	}
	return s.trustFailure(err.Error(), err)
}

func (s *Session) requireBotWorkspaceTrust(ctx context.Context, c *Client) error {
	directory := s.opts.Directory
	state, err := readWorkspaceTrust(ctx, c, directory)
	if err != nil {
		return s.trustFailure(err.Error(), err)
	}
	s.mu.Lock()
	uncertain := s.binding.TrustWriteUnknown
	s.mu.Unlock()
	if uncertain && state.level != "trusted" {
		return s.trustFailure("上次工作目录信任写入结果未确认；请在 Codex 中核对原配置，Bot 不会重复写入", errors.New("workspace trust write outcome unknown"))
	}
	if state.level == "untrusted" {
		s.mu.Lock()
		s.trustBlockedReason = ""
		if s.trustApprovalID == "" {
			s.trustApprovalID = opaque(s.instance, "workspace-trust", directory, fmt.Sprint(s.epoch))
			s.state.Approvals = append(s.state.Approvals, api.Approval{
				ID: s.trustApprovalID, TitleKey: "chat.approveBotWorkspaceTrust",
				NoticeKey: "chat.botWorkspaceTrustDescription",
				Target:    directory, Status: "pending",
				Choices: []api.Choice{{ID: "accept", LabelKey: "chat.trustBotWorkspace"}, {ID: "decline", LabelKey: "chat.decline"}},
			})
		}
		s.loading = false
		s.state.Connection = "offline"
		s.state.ConnectionIssue = "workspace_trust"
		s.state.Message = "Bot 工作目录明确标记为不信任；请在审批卡中决定是否仅信任该目录。"
		s.update()
		s.mu.Unlock()
		return nil
	}
	if state.level != "trusted" {
		if err := writeBotWorkspaceTrust(ctx, c, directory); err != nil {
			return s.trustWriteFailure(err)
		}
	}
	verified, err := readWorkspaceTrust(ctx, c, directory)
	if err != nil || verified.level != "trusted" || verified.reason != "" {
		if err == nil {
			err = errors.New("Codex 未确认 Bot 工作目录的有效信任；可能受到组织策略限制")
		}
		return s.trustFailure(err.Error(), err)
	}
	s.mu.Lock()
	s.trustBlockedReason = ""
	if s.binding.TrustWriteUnknown {
		s.binding.TrustWriteUnknown = false
		if err := s.save(); err != nil {
			s.binding.TrustWriteUnknown = true
			s.mu.Unlock()
			return s.trustFailure("无法保存 Bot 工作目录信任核对结果", err)
		}
	}
	s.trustApprovalID = ""
	s.mu.Unlock()
	return nil
}

// The decision is a Bot-native approval, not a fabricated App Server request.
// Only the accept choice may write the exact private cwd. A read after a
// successful write is required before resident initialization resumes.
func (s *Session) decideWorkspaceTrust(ctx context.Context, d api.Decision) error {
	s.mu.Lock()
	if d.ID != s.trustApprovalID || s.trustApprovalID == "" {
		s.mu.Unlock()
		return errors.New("这项工作目录授权已失效")
	}
	if d.Choice != "accept" && d.Choice != "decline" {
		s.mu.Unlock()
		return errors.New("请选择当前请求提供的选项")
	}
	id, c := s.trustApprovalID, s.client
	for i := range s.state.Approvals {
		if s.state.Approvals[i].ID == id {
			s.state.Approvals[i].Status = "sending"
		}
	}
	s.update()
	s.mu.Unlock()
	if d.Choice == "decline" {
		s.finishWorkspaceTrustApproval(id, "declined")
		s.mu.Lock()
		s.trustBlockedReason = "用户拒绝信任 Bot 工作目录；插件和对话初始化已停止。"
		s.state.Message = s.trustBlockedReason
		s.update()
		s.mu.Unlock()
		return nil
	}
	if c == nil || c.Err() != nil {
		return s.trustFailure("Codex 连接已中断；请重新连接后核对工作目录信任", ErrClosed)
	}
	state, err := readWorkspaceTrust(ctx, c, s.opts.Directory)
	if err == nil && state.level != "trusted" {
		s.mu.Lock()
		uncertain := s.binding.TrustWriteUnknown
		s.mu.Unlock()
		if uncertain {
			err = errors.New("上次工作目录信任写入结果未确认；请在 Codex 中核对原配置，Bot 不会重复写入")
		} else {
			err = writeBotWorkspaceTrust(ctx, c, s.opts.Directory)
			if err != nil {
				return s.trustWriteFailure(err)
			}
		}
	}
	if err != nil {
		return s.trustFailure(err.Error(), err)
	}
	state, err = readWorkspaceTrust(ctx, c, s.opts.Directory)
	if err != nil || state.level != "trusted" || state.reason != "" {
		if err == nil {
			err = errors.New("Codex 未确认 Bot 工作目录信任；可能受到组织策略限制")
		}
		return s.trustFailure(err.Error(), err)
	}
	s.finishWorkspaceTrustApproval(id, "allowed")
	return s.connect(ctx)
}

func (s *Session) finishWorkspaceTrustApproval(id, outcome string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trustApprovalID = ""
	for i := range s.state.Approvals {
		if s.state.Approvals[i].ID == id {
			s.state.Approvals[i].Status = "resolved"
			s.state.Approvals[i].Resolution = &api.ApprovalResolution{ChoiceID: map[string]string{"allowed": "accept", "declined": "decline"}[outcome], Outcome: outcome}
		}
	}
	s.update()
}
