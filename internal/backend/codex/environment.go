package codex

import "github.com/caelis-labs/caelis-bot/internal/runtimeenv"

// Both owned runtime launch paths share the native host's identity/IPC boundary.
// User account, configuration, and tool exports remain available to the runtime.
func ownedEnvironment(input []string) []string { return runtimeenv.Clean(input) }
