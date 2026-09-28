package notebook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextIncludesMemoryAndConsumesOnlyAcceptedVersion(t *testing.T) {
	v, err := OpenVault(filepath.Join(t.TempDir(), "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	path, err := v.PrepareDream()
	if err != nil {
		t.Fatal(err)
	}
	seed, err := v.PrepareContext(t.Context())
	if err != nil || !strings.Contains(seed.Text, "MEMORY.md:") || strings.Contains(seed.Text, "HANDOFF.md (") || seed.HandoffDigest != "" {
		t.Fatal(seed, err)
	}
	first := DreamMarker("first") + "\nCompleted X. Waiting for Y."
	if err = os.WriteFile(path, []byte(first), 0600); err != nil {
		t.Fatal(err)
	}
	seed, err = v.PrepareContext(t.Context())
	if err != nil || !strings.Contains(seed.Text, first) {
		t.Fatal(seed, err)
	}
	if ready, _ := v.DreamReady("stale"); ready {
		t.Fatal("stale handoff accepted")
	}
	if ready, _ := v.DreamReady("first"); !ready {
		t.Fatal("matching handoff missing")
	}
	if err = os.WriteFile(path, []byte("user edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = v.ConsumeContext(seed); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "user edit" {
		t.Fatal("new content removed")
	}
	seed, _ = v.PrepareContext(t.Context())
	if err = v.ConsumeContext(seed); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("accepted handoff retained", err)
	}
	seed, err = v.PrepareContext(t.Context())
	if err != nil || !strings.Contains(seed.Text, "# Memory") || seed.HandoffDigest != "" {
		t.Fatal(seed, err)
	}
}

func TestContextRefusesRedirectedAndOversizedHandoff(t *testing.T) {
	v, err := OpenVault(filepath.Join(t.TempDir(), "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	outside := filepath.Join(t.TempDir(), "secret.md")
	os.WriteFile(outside, []byte("private"), 0600)
	path := filepath.Join(v.Path(), HandoffName)
	if err = os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err = v.PrepareContext(t.Context()); err == nil {
		t.Fatal("read symlink")
	}
	if _, err = v.PrepareDream(); err == nil {
		t.Fatal("wrote symlink")
	}
	os.Remove(path)
	os.WriteFile(path, []byte(strings.Repeat("x", maxContextFile+1)), 0600)
	if _, err = v.PrepareContext(t.Context()); err == nil {
		t.Fatal("unbounded injection")
	}
	os.Remove(path)
	os.Remove(filepath.Join(v.Path(), "MEMORY.md"))
	seed, err := v.PrepareContext(t.Context())
	if err != nil || !strings.Contains(seed.Text, "MEMORY.md:\n") {
		t.Fatal("deleted memory should stay empty", err)
	}
}
