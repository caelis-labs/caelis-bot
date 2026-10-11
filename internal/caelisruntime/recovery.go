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
	ErrServiceStartFailed  = errors.New("Caelis 共享服务启动未成功，稍后重试；原任务已保留")
)

// Recover uses exactly the public lifecycle's status/start semantics, including
// normal version selection and upgrades. The caller must supply the saved Store
// of an already bound Bot; startup does not own or stop the Host.
func Recover(ctx context.Context, path, store string) error {
	dir, err := Store(store)
	if err != nil {
		return err
	}
	return sharedruntime.Shared.Recover(ctx, "caelis:"+dir, func(ctx context.Context) error {
		p, err := Find(path)
		if err != nil {
			return err
		}
		state, err := serviceState(ctx, p, dir)
		if err != nil {
			return ErrServiceStateUnknown
		}
		switch state {
		case "running":
			return nil // Another client may have recovered it; retry the handshake.
		case "stopped":
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

// ConfirmedStopped is read-only preflight for a manual replacement when a
// stale discovery record cannot be reached. Unclear status never permits it.
func ConfirmedStopped(ctx context.Context, path, store string) bool {
	p, err := Find(path)
	if err != nil {
		return false
	}
	dir, err := Store(store)
	if err != nil {
		return false
	}
	state, err := serviceState(ctx, p, dir)
	return err == nil && state == "stopped"
}

func serviceState(ctx context.Context, path, store string) (string, error) {
	raw, err := run(ctx, i18n.DefaultLocale, path, "service", "status", "--store-dir", store, "--format", "json")
	if err != nil {
		return "", err
	}
	var service struct {
		State string `json:"state"`
	}
	if json.Unmarshal(raw, &service) != nil {
		return "", ErrServiceStateUnknown
	}
	return service.State, nil
}
