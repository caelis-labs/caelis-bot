package plugins

import "github.com/caelis-labs/caelis-bot/internal/secretstore"

// Windows uses the same namespace-separated FileStore with native ACLs.
// There is no legacy Windows plugin Keychain to migrate.
const oauthNativeSupported = true

func saveSecret(string, string) error   { return secretstore.ErrUnavailable }
func loadSecret(string) (string, error) { return "", secretstore.ErrUnavailable }
func deleteSecret(string) error         { return nil }
