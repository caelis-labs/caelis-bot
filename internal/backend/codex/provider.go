package codex

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (*Session) ProviderInfo() api.ProviderInfo {
	return api.ProviderInfo{ID: "codex", Name: "Codex", ConnectionKind: "local-cli",
		HelpURL:        "https://developers.openai.com/codex/cli/",
		ConnectionHint: "使用本机已登录的 Codex。自动模式优先连接本地服务，再查找 CLI；手动路径检测通过后保存。"}
}
func (*Session) ExecutionOptions() api.ExecutionOptions {
	return api.ExecutionOptions{DefaultApprovalMode: "auto", ApprovalModes: []api.ApprovalMode{
		{ID: "auto", Name: "自动审查", Description: "保持工作目录写入沙箱，由 Codex 自动审查需要额外权限的操作。"},
		{ID: "ask", Name: "由我确认", Description: "保持工作目录写入沙箱，需要额外权限时向你确认。"},
		{ID: "read-only", Name: "只读沙箱", Description: "本地命令使用只读沙箱，不申请提升权限；连接器和 Bot 工具仍遵循各自的授权。"},
		{ID: "full-access", Name: "完全访问", Description: "关闭命令沙箱并不再询问执行权限。只在你明确需要时使用。", Dangerous: true},
	}}
}
func validateExecution(v api.ExecutionSettings) error {
	if err := api.ValidateExecutionSettings(v); err != nil {
		return err
	}
	switch v.ApprovalMode {
	case "", "auto", "ask", "read-only", "full-access":
		return nil
	default:
		return errors.New("Codex 无法识别此审批方式")
	}
}
func (s *Session) ChangeRuntime(ctx context.Context, value api.RuntimeSettings, persist func() error) (api.RuntimeCheck, error) {
	if value.Runtime != "codex" {
		return api.RuntimeCheck{}, errors.New("Codex 不能接管其他后端的连接")
	}
	if value.CLIPath != "" && (!filepath.IsAbs(value.CLIPath) || filepath.Clean(value.CLIPath) != value.CLIPath) {
		return api.RuntimeCheck{}, errors.New("请输入 Codex CLI 的完整规范路径")
	}
	return s.ChangeCLI(ctx, value.CLIPath, persist)
}

var (
	_ api.Provider            = (*Session)(nil)
	_ api.SnapshotObserver    = (*Session)(nil)
	_ api.RuntimeConfigurator = (*Session)(nil)
	_ api.ExecutionProvider   = (*Session)(nil)
	_ api.AttachmentProvider  = (*Session)(nil)
	_ api.BotToolBinder       = (*Session)(nil)
)

// ValidateSettings rejects unsupported policy before any native connection starts.
func ValidateSettings(settings api.RuntimeSettings, execution api.ExecutionSettings) error {
	if settings.Runtime != "codex" {
		return errors.New("无效的 Codex 连接配置")
	}
	if settings.CLIPath != "" && !filepath.IsAbs(settings.CLIPath) {
		return errors.New("请输入 Codex CLI 的完整路径")
	}
	return validateExecution(execution)
}
