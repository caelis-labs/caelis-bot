//go:build !windows

package runtimeenv

import (
	"context"
	"strings"
)

func splitEnvironment(entry string) (string, string, bool) { return strings.Cut(entry, "=") }

func environmentKey(key string) string { return key }
func resolvePlatform(ctx context.Context, inherited []string, who account, privateHomes ...string) ([]string, Report, error) {
	return resolve(ctx, inherited, who, privateHomes...)
}
