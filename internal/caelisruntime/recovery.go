package caelisruntime

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/sharedruntime"
)

var (
	ErrServiceStateUnknown = errors.New("Caelis 服务状态尚未确认，原任务已保留")
	ErrServiceStopped      = errors.New("Caelis 服务已停止或退出原因未知，请在连接设置中启动；原任务已保留")
	ErrServiceStartFailed  = errors.New("Caelis 共享服务启动未成功，稍后重试；原任务已保留")
)

// Recover uses exactly the public lifecycle's status/start semantics, including
// normal version selection and upgrades. It does not stop or own the Host.
// allowStart requires retained discovery evidence; a deliberate shutdown removes
// discovery, and absence alone cannot distinguish shutdown from a crash.
func Recover(ctx context.Context, path, store string, allowStart bool) error {
	dir, err := Store(store)
	if err != nil {
		return err
	}
	return sharedruntime.Shared.Recover(ctx, "caelis:"+dir, func(ctx context.Context) error {
		p, err := Find(path)
		if err != nil {
			return err
		}
		raw, err := run(ctx, i18n.DefaultLocale, p, "service", "status", "--store-dir", dir, "--format", "json")
		if err != nil {
			return ErrServiceStateUnknown
		}
		var service struct {
			State string `json:"state"`
		}
		if json.Unmarshal(raw, &service) != nil {
			return ErrServiceStateUnknown
		}
		switch service.State {
		case "running":
			return nil // Another client may have recovered it; retry the handshake.
		case "stopped":
			if !allowStart {
				return ErrServiceStopped
			}
		default:
			return ErrServiceStateUnknown
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, err = run(ctx, i18n.DefaultLocale, p, "service", "start", "--store-dir", dir, "--format", "json")
		if err != nil {
			return ErrServiceStartFailed
		}
		return nil
	})
}
