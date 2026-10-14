package desktop

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func applicationInstanceID(root string) string {
	_, id := applicationIdentity()
	if (buildChannel == "development" || buildChannel == "research" || buildChannel == "onboarding" || buildChannel == "polish") && os.Getenv("CAELIS_BOT_DATA_DIR") != "" {
		digest := sha256.Sum256([]byte(root))
		return fmt.Sprintf("%s.fixture.%x", id, digest[:8])
	}
	return id
}

// Set only by the packaging build. Local builds use a separate identity;
// installable Stable and Dev tags share the release identity and user data.
var buildChannel = "development"

func applicationIdentity() (name, id string) {
	if buildChannel == "release" {
		return "Caelis Bot", "dev.caelis.bot"
	}
	if buildChannel == "research" {
		return "Caelis Bot Research", "dev.caelis.bot.research"
	}
	if buildChannel == "onboarding" {
		return "Caelis Bot Dev Onboarding", "dev.caelis.bot.dev.onboarding"
	}
	if buildChannel == "polish" {
		return "Caelis Bot Dev PR164", "dev.caelis.bot.dev.pr164"
	}
	return "Caelis Bot Dev", "dev.caelis.bot.dev"
}

// Isolated preview bundles may only run beside their worktree profile roots.
// A direct Finder launch with the real HOME fails before Runtime discovery.
func researchRootsValid(executable, home, data, codexHome, zdotdir, path string) bool {
	return isolatedPreviewRootsValid(executable, "runtime-onboarding-research", home, data, codexHome, zdotdir, path)
}

func onboardingRootsValid(executable, home, data, codexHome, zdotdir, path string) bool {
	return isolatedPreviewRootsValid(executable, "runtime-onboarding-dev", home, data, codexHome, zdotdir, path)
}

func polishRootsValid(executable, home, data, codexHome, zdotdir, path string) bool {
	return isolatedPreviewRootsValid(executable, "pr164-polish-dev", home, data, codexHome, zdotdir, path)
}

func isolatedPreviewRootsValid(executable, profileDirectory, home, data, codexHome, zdotdir, path string) bool {
	root := filepath.Clean(filepath.Join(filepath.Dir(executable), "../../../.."))
	base := filepath.Join(root, ".cache", profileDirectory)
	profileRoot := filepath.Dir(home)
	legacy := profileRoot == base
	profile, err := filepath.Rel(filepath.Join(base, "profiles"), profileRoot)
	named := err == nil && profile != "." && profile != ".." && !strings.Contains(profile, string(filepath.Separator)) && !strings.HasPrefix(profile, "..")
	if !legacy && !named {
		return false
	}
	if named {
		for _, c := range profile {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	expectedHome := filepath.Join(profileRoot, "home")
	expectedData := filepath.Join(profileRoot, "data")
	resolvedRoot, rootErr := filepath.EvalSymlinks(base)
	resolvedHome, homeErr := filepath.EvalSymlinks(home)
	resolvedData, dataErr := filepath.EvalSymlinks(data)
	resolvedCodex, codexErr := filepath.EvalSymlinks(codexHome)
	if rootErr != nil || homeErr != nil || dataErr != nil || codexErr != nil {
		return false
	}
	for _, candidate := range []string{resolvedHome, resolvedData, resolvedCodex} {
		contained, relErr := filepath.Rel(resolvedRoot, candidate)
		if relErr != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
			return false
		}
	}
	expectedPath := strings.Join([]string{filepath.Join(expectedHome, ".local", "bin"), filepath.Join(expectedHome, "bin"), "/usr/bin", "/bin", "/usr/sbin", "/sbin"}, string(os.PathListSeparator))
	return filepath.Clean(home) == expectedHome && filepath.Clean(data) == expectedData && filepath.Clean(codexHome) == filepath.Join(expectedHome, ".codex") && filepath.Clean(zdotdir) == expectedHome && path == expectedPath
}
