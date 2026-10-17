package caelisruntime

import (
	"context"
	"os"

	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/sharedruntime"
)

func ownBundledPath() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	return bundledPath(executable)
}

// SameFile also catches an explicit symlink/hardlink to the bundled executable:
// selecting it manually must not enable a self-update inside the signed app.
func isBundled(path string) bool {
	a, err := os.Stat(path)
	if err != nil {
		return false
	}
	b, err := os.Stat(ownBundledPath())
	return err == nil && os.SameFile(a, b)
}

// PrepareBundled offers the pinned CLI to Core's public lifecycle once per
// recovery interval. Core keeps busy compatible Hosts and schedules their idle
// upgrade. Bot never kills a Host or keeps its own executable selection record.
func PrepareBundled(ctx context.Context, path, store string) error {
	if ownBundledPath() == "" {
		return nil
	}
	p, err := Find(path)
	if err != nil {
		return err
	}
	if !isBundled(p) {
		return nil
	}
	dir, err := Store(store)
	if err != nil {
		return err
	}
	return sharedruntime.Shared.Recover(ctx, "caelis-bundled:"+dir, func(ctx context.Context) error {
		_, err := run(ctx, i18n.DefaultLocale, p, "service", "start", "--store-dir", dir, "--format", "json")
		return err
	})
}
