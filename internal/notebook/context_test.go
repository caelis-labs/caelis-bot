package notebook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextUsesMemoryAndLegacyHandoffRequiresOriginalReceipt(t *testing.T) {
	v, err := OpenVault(filepath.Join(t.TempDir(), "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	path, err := v.PrepareDream()
	if err != nil {
		t.Fatal(err)
	}
	legacy := DreamMarker("original-call") + "\nCurrent goal and pending task receipt."
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	seed, err := v.PrepareContext(t.Context())
	if err != nil || !strings.Contains(seed.Text, "MEMORY.md:") || strings.Contains(seed.Text, legacy) || seed.HandoffDigest != "" {
		t.Fatal("legacy handoff entered normal context", seed, err)
	}
	if _, err := v.LegacyDreamHandoff("other-call"); err == nil {
		t.Fatal("accepted wrong receipt")
	}
	text, err := v.LegacyDreamHandoff("original-call")
	if err != nil || text != "Current goal and pending task receipt." {
		t.Fatal(text, err)
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != legacy {
		t.Fatal("legacy user data changed", err)
	}
}

func TestContextIgnoresUntrustedLegacyHandoffAndRejectsUnsafeMemory(t *testing.T) {
	v, err := OpenVault(filepath.Join(t.TempDir(), "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	outside := filepath.Join(t.TempDir(), "secret.md")
	os.WriteFile(outside, []byte("private"), 0600)
	path := filepath.Join(v.Path(), HandoffName)
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := v.PrepareContext(t.Context()); err != nil {
		t.Fatal("legacy file should not enter context", err)
	}
	if _, err := v.LegacyDreamHandoff("call"); err == nil {
		t.Fatal("read redirected legacy file")
	}
	os.Remove(path)
	memory := filepath.Join(v.Path(), "MEMORY.md")
	os.Remove(memory)
	os.Symlink(outside, memory)
	if _, err := v.PrepareContext(t.Context()); err == nil {
		t.Fatal("read redirected memory")
	}
}
