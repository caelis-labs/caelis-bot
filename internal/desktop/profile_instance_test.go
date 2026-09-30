package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitProfileInstanceCanonicalAliasesAndDefault(t *testing.T) {
	root := t.TempDir()
	defaultProfile := filepath.Join(root, "default")
	profile := filepath.Join(root, "isolated")
	for _, directory := range []string{defaultProfile, profile} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(profile, alias); err != nil {
		t.Fatal(err)
	}
	key, err := profileInstanceID("fixture.app", profile, defaultProfile)
	if err != nil || key == "fixture.app" || strings.Contains(key, root) {
		t.Fatal("explicit profile is not privately isolated", key, err)
	}
	for _, path := range []string{alias, profile + string(filepath.Separator) + "."} {
		got, err := profileInstanceID("fixture.app", path, defaultProfile)
		if err != nil || got != key {
			t.Fatal("same profile alias bypassed its owner", got, err)
		}
	}
	// Where the filesystem resolves a case alias, it must preserve the owner too.
	caseAlias := filepath.Join(root, "ISOLATED")
	if info, err := os.Stat(caseAlias); err == nil && info.IsDir() {
		got, err := profileInstanceID("fixture.app", caseAlias, defaultProfile)
		if err != nil || got != key {
			t.Fatal("case alias bypassed its owner", got, err)
		}
	}
	missingChild := filepath.Join(profile, "future", "profile")
	missingAlias := filepath.Join(alias, "future", "profile")
	one, err := profileInstanceID("fixture.app", missingChild, defaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	two, err := profileInstanceID("fixture.app", missingAlias, defaultProfile)
	if err != nil || one != two {
		t.Fatal("missing child below symlink bypassed its owner", err)
	}
	defaultAlias := filepath.Join(root, "default-alias")
	if err := os.Symlink(defaultProfile, defaultAlias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{defaultProfile, defaultAlias} {
		got, err := profileInstanceID("fixture.app", path, defaultProfile)
		if err != nil || got != "fixture.app" {
			t.Fatal("explicit default bypassed default instance", got, err)
		}
	}
	broken := filepath.Join(root, "broken")
	if err := os.Symlink(filepath.Join(root, "missing"), broken); err != nil {
		t.Fatal(err)
	}
	if _, err := profileInstanceID("fixture.app", broken, defaultProfile); err == nil {
		t.Fatal("unresolved symlink accepted a distinct owner key")
	}
}
