package remotework

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateOwnerRejectsRedirectedAndPublicDirectory(t *testing.T) {
	root := t.TempDir()
	public := filepath.Join(root, "public")
	if err := os.Mkdir(public, 0755); err != nil {
		t.Fatal(err)
	}
	if privateDir(public) == nil {
		t.Fatal("accepted a public owner directory")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if privateDir(link) == nil {
		t.Fatal("accepted a redirected owner directory")
	}
}
func TestCorruptNodeModelNeverFallsBackToDefault(t *testing.T) {
	o := &Owner{root: t.TempDir()}
	if err := os.Mkdir(filepath.Join(o.root, "codex"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := o.model("codex"); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"broken", "null", `{"effort":"invalid"}`} {
		if err := os.WriteFile(filepath.Join(o.root, "codex", "model.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := o.model("codex"); err == nil {
			t.Fatal("replaced an unreadable model selection")
		}
	}
}
func TestOwnerSocketDoesNotUseLongProfilePath(t *testing.T) {
	a := Socket("/home/user/" + strings.Repeat("long-profile/", 30))
	b := Socket("/home/other/" + strings.Repeat("long-profile/", 30))
	if len(a) > 103 || a == b {
		t.Fatal("socket exceeds native limit or aliases profiles")
	}
}
