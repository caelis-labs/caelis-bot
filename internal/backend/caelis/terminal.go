package caelis

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

// WorkTerminal resolves only a confirmed, owned native Worker. The terminal
// authenticates as the local user; Bot execution retains its application scope.
func (s *Session) WorkTerminal(ctx context.Context, id string) (api.TerminalTarget, error) {
	if err := ctx.Err(); err != nil {
		return api.TerminalTarget{}, err
	}
	s.mu.Lock()
	w, ok := s.state.Workers[id]
	life, instance := s.state.Connection, s.state.InstanceID
	ready := s.connected && !s.closed && s.client != nil
	s.mu.Unlock()
	if !ok || !w.Native || w.Binding.SessionId == "" || w.Binding.ApplicationId != life.ApplicationId || w.Binding.ConnectionId != life.ConnectionId || w.Binding.PrincipalId != life.PrincipalId {
		return api.TerminalTarget{}, errors.New("该任务尚无已确认的共享会话，不会自动重建")
	}
	if !ready {
		return api.TerminalTarget{}, errors.New("任务连接尚未就绪，请重新连接后打开")
	}
	d, _, err := Discover(s.settings)
	if err != nil {
		return api.TerminalTarget{}, err
	}
	if d.InstanceID != instance {
		return api.TerminalTarget{}, errors.New("Caelis 服务已变化，请等待重新连接后打开")
	}
	binary, err := caelisruntime.Find(s.settings.CLIPath)
	if err != nil {
		return api.TerminalTarget{}, err
	}
	store, err := caelisruntime.Store(s.settings.CaelisStore)
	if err != nil {
		return api.TerminalTarget{}, err
	}
	return api.TerminalTarget{Runtime: "caelis", Binary: binary, Endpoint: d.Endpoint, Session: w.Binding.SessionId, Directory: w.Task.Workspace, Store: store, TokenFile: filepath.Join(store, "runtime/service/auth.token")}, nil
}
