package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDevelopmentAndReleaseIdentityAndDataStaySeparate(t *testing.T) {
	old := buildChannel
	t.Cleanup(func() { buildChannel = old })
	// Preserve any caller override while exercising the default profile path.
	t.Setenv("CAELIS_BOT_DATA_DIR", "")
	_ = os.Unsetenv("CAELIS_BOT_DATA_DIR")
	config, err := nativeDataDirectoryBase()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ channel, name, id string }{
		{"development", "Caelis Bot Dev", "dev.caelis.bot.dev"},
		{"release", "Caelis Bot", "dev.caelis.bot"},
	} {
		buildChannel = tc.channel
		name, id := applicationIdentity()
		if name != tc.name || id != tc.id {
			t.Fatal("unexpected application identity")
		}
		path, err := applicationDataDirectory()
		if err != nil || path != filepath.Join(config, tc.name) {
			t.Fatal("wrong default profile", err)
		}
	}
}
