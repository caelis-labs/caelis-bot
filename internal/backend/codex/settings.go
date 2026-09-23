package codex

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Validation and replacement serialize with Submit/Interrupt. The stored path
// is changed only after an independent, read-only protocol handshake succeeds.
func (s *Session) ChangeCLI(ctx context.Context, path string, persist func() error) (api.RuntimeCheck, error) {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	busy := s.run != "" || len(s.childRuns) > 0 || s.binding.Pending != nil || len(s.prompts) > 0 || s.closed || s.closing || s.state.Phase == "unknown"
	s.mu.Unlock()
	if busy {
		return api.RuntimeCheck{}, errors.New("请先结束当前工作或处理待确认事项，再切换连接")
	}
	ctx, cancel := s.operation(ctx, 30*time.Second)
	defer cancel()
	if err := os.MkdirAll(s.opts.Directory, 0700); err != nil {
		return api.RuntimeCheck{}, errors.New("无法准备连接，请重试")
	}
	probe, err := Start(ctx, Options{Binary: path, Socket: s.opts.Socket, Directory: s.opts.Directory, CLIOnly: path != ""})
	if err != nil {
		switch {
		case errors.Is(err, errRuntimeMissing):
			return api.RuntimeCheck{}, errors.New("没有找到可执行的 Codex CLI，请检查路径")
		case incompatibleProtocol(err):
			return api.RuntimeCheck{}, errors.New("此 Codex 不支持当前需要的连接协议，原有配置未更改")
		default:
			return api.RuntimeCheck{}, errors.New("Codex 握手未通过，原有配置未更改")
		}
	}
	_, err = probe.ReadAuthStatus(ctx)
	probe.Close()
	if err != nil {
		if incompatibleProtocol(err) {
			return api.RuntimeCheck{}, errors.New("此 Codex 不支持所需的账户接口，原有配置未更改")
		}
		return api.RuntimeCheck{}, errors.New("无法读取 Codex 账户状态，原有配置未更改")
	}
	if err = persist(); err != nil {
		return api.RuntimeCheck{}, err
	}
	s.mu.Lock()
	s.opts.Binary = path
	s.state.Connection = "offline"
	s.update()
	s.mu.Unlock()
	result := api.RuntimeCheck{Saved: true, Message: "检测通过，配置已保存"}
	if err = s.connect(ctx); err != nil {
		result.Message = "配置已保存，暂时无法恢复连接，可在聊天中重试"
		return result, nil
	}
	view := s.Snapshot()
	result.Connected = view.Connection == "ready"
	if result.Connected {
		result.Message = "检测通过，已保存并连接 Codex"
	} else {
		result.Message = "检测通过，配置已保存；请在聊天中登录 Codex"
	}
	return result, nil
}
