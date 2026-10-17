//go:build !windows

package runtimeenv

import "context"

func environmentKey(key string) string { return key }
func resolvePlatform(ctx context.Context, inherited []string, who account, privateHomes ...string) ([]string, Report, error) {
	return resolve(ctx, inherited, who, privateHomes...)
}
