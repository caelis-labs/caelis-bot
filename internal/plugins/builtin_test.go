package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopWorldInventoryUsesVerifiedPackageVersion(t *testing.T) {
	root := t.TempDir()
	body := []byte(`{"$schema":"` + manifestSchema + `","name":"desktop-world","version":"0.1.1"}`)
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	entry := Entry{ID: "desktop-world", Version: "0.1.1", Source: "bundled:desktop-world/v0.1.1", Builtin: true, Files: map[string]string{"plugin.json": hex.EncodeToString(sum[:])}}
	check := func(e Entry) error {
		t.Helper()
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		_, err = readDesktopWorldInventory(root, raw)
		return err
	}
	if err := check(entry); err != nil {
		t.Fatal("new signed-bundle version was rejected", err)
	}
	for _, version := range []string{"", "../0.1.1", "0.1.2"} {
		changed := entry
		changed.Version, changed.Source = version, "bundled:desktop-world/v"+version
		if err := check(changed); err == nil {
			t.Fatalf("invalid or mismatched version accepted: %q", version)
		}
	}
	wrongSource := entry
	wrongSource.Source = "bundled:desktop-world/v0.1.0"
	if err := check(wrongSource); err == nil {
		t.Fatal("mismatched source accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := check(entry); err == nil {
		t.Fatal("tampered inventory member accepted")
	}
}
