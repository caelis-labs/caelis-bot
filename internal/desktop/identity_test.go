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
		{"research", "Caelis Bot Research", "dev.caelis.bot.research"},
		{"onboarding", "Caelis Bot Dev Onboarding", "dev.caelis.bot.dev.onboarding"},
		{"polish", "Caelis Bot Dev PR164", "dev.caelis.bot.dev.pr164"},
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
func TestResearchIdentityRequiresExactIsolatedRoots(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "dist", "Caelis Bot Research.app", "Contents", "MacOS", "caelis-bot")
	home := filepath.Join(root, ".cache", "runtime-onboarding-research", "home")
	data := filepath.Join(root, ".cache", "runtime-onboarding-research", "data")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	path := home + "/.local/bin:" + home + "/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	if !researchRootsValid(exe, home, data, home+"/.codex", home, path) {
		t.Fatal("isolated launch rejected")
	}
	if researchRootsValid(exe, os.Getenv("HOME"), data, home+"/.codex", home, path) {
		t.Fatal("real account HOME accepted")
	}
	if researchRootsValid(exe, home, data, home+"/.codex", home, path+":/opt/homebrew/bin") {
		t.Fatal("global CLI path accepted")
	}
}

func TestOnboardingDevIdentityRequiresExactIsolatedRoots(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "dist", "Caelis Bot Dev Onboarding.app", "Contents", "MacOS", "caelis-bot")
	home := filepath.Join(root, ".cache", "runtime-onboarding-dev", "profiles", "fresh", "home")
	data := filepath.Join(root, ".cache", "runtime-onboarding-dev", "profiles", "fresh", "data")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	path := home + "/.local/bin:" + home + "/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	if !onboardingRootsValid(exe, home, data, home+"/.codex", home, path) {
		t.Fatal("isolated onboarding launch rejected")
	}
	if onboardingRootsValid(exe, os.Getenv("HOME"), data, home+"/.codex", home, path) ||
		onboardingRootsValid(exe, home, data, home+"/.codex", home, path+":/opt/homebrew/bin") ||
		researchRootsValid(exe, home, data, home+"/.codex", home, path) {
		t.Fatal("unsafe or cross-channel onboarding roots accepted")
	}
	if err := os.Rename(data, data+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(os.Getenv("HOME"), data); err != nil {
		t.Fatal(err)
	}
	if onboardingRootsValid(exe, home, data, home+"/.codex", home, path) {
		t.Fatal("symlinked data directory outside profile accepted")
	}
}

func TestPR164DevIdentityRequiresExactIsolatedRoots(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "dist", "Caelis Bot Dev PR164.app", "Contents", "MacOS", "caelis-bot")
	home := filepath.Join(root, ".cache", "pr164-polish-dev", "profiles", "review", "home")
	data := filepath.Join(root, ".cache", "pr164-polish-dev", "profiles", "review", "data")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	path := home + "/.local/bin:" + home + "/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	if !polishRootsValid(exe, home, data, home+"/.codex", home, path) {
		t.Fatal("isolated PR164 launch rejected")
	}
	if polishRootsValid(exe, os.Getenv("HOME"), data, home+"/.codex", home, path) ||
		polishRootsValid(exe, home, data, home+"/.codex", home, path+":/opt/homebrew/bin") ||
		onboardingRootsValid(exe, home, data, home+"/.codex", home, path) {
		t.Fatal("unsafe or cross-channel PR164 roots accepted")
	}
}
