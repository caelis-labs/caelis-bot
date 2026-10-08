//go:build windows

package plugins

import "github.com/caelis-labs/caelis-bot/internal/secretstore"

func lockOAuth(string, string) (func(), error) { return nil, secretstore.ErrUnavailable }
