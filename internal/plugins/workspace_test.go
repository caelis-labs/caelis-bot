package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitDevelopmentStoreReturnsToShippedLayout(t *testing.T) {
	appRoot := t.TempDir()
	draft := filepath.Join(appRoot, "PluginState")
	manager, err := Open(draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Mutate(t.Context(), "markdown-work", "install", nil); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(appRoot, "Notebook", "plugins", "data")
	if err = os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(data, "user.txt"), []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		stable, err := OpenWorkspace(appRoot)
		if err != nil {
			t.Fatal(err)
		}
		if len(stable.Selection().SkillRoots) != 1 {
			t.Fatal("lost installed Skill")
		}
		body, err := os.ReadFile(filepath.Join(stable.Root(), "data", "user.txt"))
		if err != nil || string(body) != "retain" {
			t.Fatal("lost plugin data", err)
		}
	}
}

func TestPinnedPluginUpdateSelectsOnlyReviewedVersion(t *testing.T) {
	m, err := OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.state.Installed["markdown-work"] = installed{Version: "0.9.0", Digest: "previous", Enabled: true}
	if err := m.save(m.state); err != nil {
		t.Fatal(err)
	}
	previousRevision := m.state.Revision
	if _, err := m.MutateVersion(t.Context(), "markdown-work", "update", "0.9.0", nil); err == nil || m.state.Revision != previousRevision {
		t.Fatal("outdated pin changed plugin state")
	}
	if _, err := m.MutateVersion(t.Context(), "markdown-work", "update", "1.0.0", nil); err != nil {
		t.Fatal(err)
	}
	if got := m.Selection(); got.Revision != previousRevision+1 || len(got.SkillRoots) != 1 {
		t.Fatal("reviewed update did not project its Skill")
	}
}
