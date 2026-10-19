package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsShellIntegrationRequiresOwnedSetup(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "caelis-bot.exe")
	const id = "dev.caelis.bot"
	if windowsShellIntegration(exe, id) {
		t.Fatal("unpackaged application registered shell integration")
	}
	marker := "[CaelisBot]\nIdentity=" + id + "\nLocation=" + dir + "\n"
	if err := os.WriteFile(filepath.Join(dir, "installation.ini"), []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	if !windowsShellIntegration(exe, id) || windowsShellIntegration(exe, "dev.caelis.bot.preview") {
		t.Fatal("Setup ownership was not checked")
	}
	// A ZIP marker wins even if a stale Setup marker is present.
	if err := os.WriteFile(filepath.Join(dir, "portable.json"), []byte(`{"schema":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if windowsShellIntegration(exe, id) {
		t.Fatal("ZIP registered shell integration")
	}
}
