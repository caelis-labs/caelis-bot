package desktop

// Set only by the packaging build. Development and release installs must never
// share a single-instance owner, data directory or macOS permission identity.
var buildChannel = "development"

func applicationIdentity() (name, id string) {
	if buildChannel == "release" {
		return "Caelis Bot", "dev.caelis.bot"
	}
	return "Caelis Bot Dev", "dev.caelis.bot.dev"
}
