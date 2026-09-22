package codex

import "strings"

// A standalone Bot is not a nested turn of whichever developer agent happened
// to launch it. Preserve account/config selection, never inherit host IPC,
// thread/session identity, originator overrides or the caller's tool runtime.
func ownedEnvironment(input []string) []string {
	out := make([]string, 0, len(input))
	for _, entry := range input {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "CODEX_") && key != "CODEX_HOME" && key != "CODEX_API_KEY" {
			continue
		}
		out = append(out, entry)
	}
	return out
}
