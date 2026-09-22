package codex

import (
	"slices"
	"testing"
)

func TestOwnedEnvironmentDoesNotInheritCallerIdentityOrIPC(t *testing.T) {
	want := []string{"PATH=/bin", "CODEX_HOME=/private/config", "CODEX_API_KEY=synthetic", "OPENAI_API_KEY=synthetic"}
	in := append(slices.Clone(want), "CODEX_THREAD_ID=foreign", "CODEX_SESSION_ID=foreign", "CODEX_APP_TOOLS_PIPE_PATH=/private/foreign", "CODEX_INTERNAL_ORIGINATOR_OVERRIDE=desktop", "CODEX_SANDBOX=caller")
	if got := ownedEnvironment(in); !slices.Equal(got, want) {
		t.Fatal("standalone owner inherited caller context")
	}
}
