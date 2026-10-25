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
		{"release", "Caelis Bot", "dev.caelis.bot"},
		{"onboarding", "Caelis Bot Onboarding", "dev.caelis.bot.onboarding"},
	} {
		buildChannel = tc.channel
		name, id := applicationIdentity()
		if name != tc.name || id != tc.id {
			t.Fatal("unexpected application identity")
		}
		path, err := applicationDataDirectory()
		expected := filepath.Join(config, tc.name)
		if p, e := developmentDefault(config); e == nil && p.DataDirectory != "" {
			expected = p.DataDirectory
		}
		if err != nil || path != expected {
			t.Fatal("wrong default profile", err)
		}
	}
}
func TestDevelopmentAliasAndPhysicalProfileShareOneInstance(t *testing.T) {
	old := buildChannel
	t.Cleanup(func() { buildChannel = old })
	buildChannel = "development"
	root := t.TempDir()
	actual := filepath.Join(root, "Daily")
	os.MkdirAll(actual, 0700)
	alias := filepath.Join(root, "old-profile")
	if e := os.Symlink(actual, alias); e != nil {
		t.Skipf("OS does not permit symlink fixture: %v", e)
	}
	t.Setenv("CAELIS_BOT_DATA_DIR", actual)
	if applicationInstanceID(actual) != applicationInstanceID(alias) {
		t.Fatal("alias can create second writer on same profile")
	}
	other := filepath.Join(root, "another")
	os.MkdirAll(other, 0700)
	if applicationInstanceID(other) == applicationInstanceID(actual) {
		t.Fatal("independent profiles share a lock")
	}
	buildChannel = "release"
	if applicationInstanceID(alias) != "dev.caelis.bot" {
		t.Fatal("release identity adopted profile")
	}
}
