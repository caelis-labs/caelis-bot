package desktop

import (
	"crypto/sha256"
	"fmt"
	"os"
)

func applicationInstanceID(root string) string {
	_, id := applicationIdentity()
	if buildChannel == "development" && os.Getenv("CAELIS_BOT_DATA_DIR") != "" {
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
	return "Caelis Bot Dev", "dev.caelis.bot.dev"
}
