package plugins

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillDiscoveryAcceptsWindowsMetadataWithoutRewritingReviewedBytes(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		root := t.TempDir()
		dir := filepath.Join(root, "skills", "desktop-world")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "SKILL.md")
		original := []byte(strings.ReplaceAll("---\nname: desktop-world\ndescription: Fixture only\n---\n\nBody\n", "\n", newline))
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		p := Package{Root: root}
		if err := loadSkills(&p); err != nil || len(p.Skills) != 1 || p.Skills[0] != dir || len(p.Issues) != 0 {
			t.Fatal("valid Skill was omitted", err, p.Issues)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(original, after) {
			t.Fatal("package bytes changed", err)
		}
	}
}
