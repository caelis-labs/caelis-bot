package caelisruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsBundledPathIsBuildOwned(t *testing.T) {
	previous := windowsBundled
	t.Cleanup(func() { windowsBundled = previous })
	app := filepath.Join(t.TempDir(), "Bot Preview.exe")
	if err := os.WriteFile(app, []byte("path fixture only"), 0600); err != nil {
		t.Fatal(err)
	}
	windowsBundled = "0"
	if bundledPath(app) != "" {
		t.Fatal("ordinary tool inferred an embedded runtime")
	}
	windowsBundled = "1"
	want := filepath.Join(filepath.Dir(app), "Helpers", "caelis.exe")
	if got := bundledPath(app); got != want {
		t.Fatal("bundle path", got)
	}
	if _, err := Find(want); err == nil {
		t.Fatal("damaged bundle silently succeeded")
	}
}
