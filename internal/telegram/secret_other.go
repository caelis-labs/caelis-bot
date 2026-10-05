//go:build !darwin || !cgo

package telegram

import "errors"

func saveSecret(string, string) error   { return errors.New("keychain unavailable") }
func loadSecret(string) (string, error) { return "", errors.New("keychain unavailable") }
func deleteSecret(string) error         { return nil }
