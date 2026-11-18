package codex

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Setup can inspect an inactive profile. A matching public socket is safe to
// borrow even when the settings client and resident use different CLI builds.
func (s *Session) catalogClient() *Client {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Connection == "ready" && !s.closed && !s.closing {
		return s.client
	}
	return nil
}

func sharedModels(ctx context.Context, binary, home string, candidate *Client, start func(context.Context, Options) (*Client, error)) ([]api.ModelOption, error) {
	socket := controlSocket(home)
	if socket == "" {
		return nil, errModelCatalog
	}
	if candidate != nil {
		if conn, ok := candidate.rpc.conn.(*socketConnection); ok && conn.endpoint == "unix://"+filepath.ToSlash(socket) {
			return candidate.models(ctx)
		}
	}
	if start == nil {
		start = Start
	}
	c, err := start(ctx, Options{Binary: binary, Socket: socket, RequiredSocket: true, RecoverShared: true})
	if err != nil {
		return nil, errModelCatalog
	}
	defer c.Close()
	return c.models(ctx)
}

var errModelCatalog = errors.New("Codex 模型目录暂不可用，请刷新；不会自动换用其他模型")
