package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitDevelopmentProfileIsolatesInstanceLockOnly(t *testing.T) {
	old := buildChannel
	t.Cleanup(func() { buildChannel = old })
	t.Setenv("CAELIS_BOT_DATA_DIR", "/private/tmp/fixture")
	buildChannel = "development"
	a, b := applicationInstanceID("/private/tmp/fixture"), applicationInstanceID("/private/tmp/other")
	if a == b || a != applicationInstanceID("/private/tmp/fixture") {
		t.Fatal("fixture instance isolation missing")
	}
	buildChannel = "release"
	if applicationInstanceID("/private/tmp/fixture") != "dev.caelis.bot" {
		t.Fatal("release identity changed")
	}
}

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
		{"dev", "Caelis Bot Dev Release", "dev.caelis.bot.devrelease"},
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
