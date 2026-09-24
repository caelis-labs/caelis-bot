package codex

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
)

func (s *Session) WorkTerminal(ctx context.Context, id string) (api.TerminalTarget, error) {
	if err := ctx.Err(); err != nil {
		return api.TerminalTarget{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.binding.Tasks[id]
	if task == nil || task.Thread == "" {
		return api.TerminalTarget{}, errors.New("该任务尚无已确认的运行时绑定，不会自动重建")
	}
	if s.closed || s.closing || s.client == nil || s.state.Connection != "ready" {
		return api.TerminalTarget{}, errors.New("任务连接尚未就绪，请重新连接后打开")
	}
	endpoint, ok := s.client.rpc.conn.(interface{ terminalEndpoint() string })
	if !ok || endpoint.terminalEndpoint() == "" {
		return api.TerminalTarget{}, errors.New("当前 Codex 连接没有可用的终端入口")
	}
	binary, err := runtimeBinary(s.opts.Binary)
	if err != nil {
		return api.TerminalTarget{}, errors.New("打开终端需要本机 Codex CLI，请在连接设置中选择安装")
	}
	home := s.client.home
	if !filepath.IsAbs(home) {
		home = os.Getenv("CODEX_HOME")
	}
	return api.TerminalTarget{Runtime: "codex", Binary: binary, Endpoint: endpoint.terminalEndpoint(), Thread: task.Thread, Directory: task.View.Workspace, CodexHome: home}, nil
}
