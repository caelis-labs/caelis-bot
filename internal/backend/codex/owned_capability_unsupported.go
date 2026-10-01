//go:build (!darwin || !cgo) && !linux

package codex

// OwnedRuntimeSupported reports compiled native fencing support. A supported
// build still requires the actual owned handshake and native power binding.
func OwnedRuntimeSupported() bool { return false }
