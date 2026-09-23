package desktop

import (
	"path/filepath"
	"testing"
)

func TestExplicitDataDirectoryCannotFallBackToDailyProfile(t *testing.T) {
	for _, invalid := range []string{"", "relative-profile"} {
		t.Setenv("CAELIS_BOT_DATA_DIR", invalid)
		if _, err := applicationDataDirectory(); err == nil {
			t.Fatal("invalid explicit profile accepted")
		}
	}
	root := filepath.Join(t.TempDir(), "isolated")
	t.Setenv("CAELIS_BOT_DATA_DIR", root)
	got, err := applicationDataDirectory()
	if err != nil || got != root {
		t.Fatal("profile did not remain isolated", err)
	}
}
