package codex

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func openShared(ctx context.Context, binary, socket string, start func(context.Context, Options) (*Client, error)) (*Client, error) {
	if start == nil {
		start = Start
	}
	return start(ctx, Options{Binary: binary, Socket: socket, RequiredSocket: true, RecoverShared: true})
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
	c, err := openShared(ctx, binary, socket, start)
	if err != nil {
		return nil, errModelCatalog
	}
	defer c.Close()
	return c.models(ctx)
}

var errModelCatalog = errors.New("Codex 模型目录暂不可用，请刷新；不会自动换用其他模型")
