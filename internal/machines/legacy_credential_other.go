//go:build !darwin || !cgo

package machines

// Legacy non-macOS profiles had no platform Keychain account.
func DeleteLegacyCredential(string) error { return nil }
