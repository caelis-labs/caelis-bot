package caelisruntime

import (
	"context"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/sharedruntime"
)

// The executable's own bundle is the only embedded-runtime location. Never
// search another app, the working directory, or a writable configuration file.
func bundledPath(executable string) string {
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return ""
	}
	macos := filepath.Dir(resolved)
	contents := filepath.Dir(macos)
	if filepath.Base(macos) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(filepath.Dir(contents)) != ".app" {
		return ""
	}
	return filepath.Join(contents, "Helpers", "caelis")
}

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
