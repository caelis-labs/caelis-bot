package desktop

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
)

func applicationInstanceID(root string) string {
	_, id := applicationIdentity()
	if buildChannel != "release" && customInstanceProfile(root) {
		// Native assembly creates the directory before instance registration. A
		// retained alias and its physical target must own the same instance lock;
		// this affects only lock identity, never persisted task/owner paths.
		root = filepath.Clean(root)
		if canonical, err := filepath.EvalSymlinks(root); err == nil {
			root = canonical
		}
		digest := sha256.Sum256([]byte(root))
		return fmt.Sprintf("%s.fixture.%x", id, digest[:8])
	}
	return id
}

// Set only by the packaging build. Local builds use a separate identity;
// installable Stable and Dev tags share the release identity and user data.
var buildChannel = "development"

func applicationIdentity() (name, id string) {
	if buildChannel == "onboarding" {
		return "Caelis Bot Onboarding", "dev.caelis.bot.onboarding"
	}
	if buildChannel == "release" {
		return "Caelis Bot", "dev.caelis.bot"
	}
	if buildChannel == "windows-preview" {
		return "Caelis Bot Preview", "dev.caelis.bot.preview"
	}
	return "Caelis Bot Dev", "dev.caelis.bot.dev"
}
