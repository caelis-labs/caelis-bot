package backend

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// AuthorizeCaelisFullAccess is invoked only from the App's explicit
// confirmation for one future scope. The Caelis adapter authenticates the
// Host principal and verifies both Host and Application readbacks before a
// separate preference save can follow.
func (s *Service) AuthorizeCaelisFullAccess(scope string) error {
	if scope != "bot" && scope != "worker" {
		return errors.New("完全访问授权作用域无效")
	}
	// Both scopes share one Host Connection. Serialize their exact-state CAS
	// requests so independent choices made in this App cannot race each other.
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	ctx, cancel := s.machineContext()
	defer cancel()
	if scope == "worker" {
		if s.localWorkers == nil {
			return errors.New("新任务连接不可用")
		}
		authorizer, ok := s.localWorkers.(interface{ AuthorizeCaelisFullAccess(context.Context) error })
		if !ok {
			return errors.New("新任务连接无法核对 Host 授权")
		}
		return authorizer.AuthorizeCaelisFullAccess(ctx)
	}
	engine := s.currentEngine()
	provider, ok := engine.(api.Provider)
	if !ok || provider.ProviderInfo().ID != "caelis" {
		return errors.New("请先连接 Caelis 作为 Bot 对话连接")
	}
	authorizer, ok := engine.(interface {
		AuthorizeFullAccess(context.Context, string) error
	})
	if !ok {
		return errors.New("当前 Caelis Bot 连接无法核对 Host 授权")
	}
	return authorizer.AuthorizeFullAccess(ctx, "bot")
}
