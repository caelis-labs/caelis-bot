//go:build (!darwin || !cgo) && !windows

package plugins

import "github.com/caelis-labs/caelis-bot/internal/secretstore"

const oauthNativeSupported = false

func saveSecret(string, string) error   { return secretstore.ErrUnavailable }
func loadSecret(string) (string, error) { return "", secretstore.ErrUnavailable }
func deleteSecret(string) error         { return nil }
