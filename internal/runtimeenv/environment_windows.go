package runtimeenv

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

func environmentKey(key string) string { return strings.ToUpper(key) }

// Explorer provides the account environment directly. Do not execute POSIX
// login-shell probes or PowerShell profiles merely to start the desktop app.
func resolvePlatform(_ context.Context, inherited []string, who account, privateHomes ...string) ([]string, Report, error) {
	v := values(Clean(inherited))
	report := Report{}
	private := func(path string) bool {
		for _, known := range privateHomes {
			if known != "" && strings.EqualFold(filepath.Clean(path), filepath.Clean(known)) {
				return true
			}
		}
		return false
	}
	for _, key := range []string{"HOME", "USERPROFILE"} {
		if !filepath.IsAbs(v[key]) || private(v[key]) {
			if !filepath.IsAbs(who.home) {
				return environment(v), report, errors.New("account home could not be resolved")
			}
			v[key] = who.home
			report.HomeRestored = true
		}
	}
	return environment(v), report, nil
}
