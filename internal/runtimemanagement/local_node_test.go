package runtimemanagement

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLocalNodeInstallerSupportsPrivateProfileOutsideHOMEWithoutRemoteExpansion(t *testing.T) {
	node, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(node, 0700); e != nil {
		t.Fatal(e)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	m, e := NewLocalNode(node)
	if e != nil {
		t.Fatal(e)
	}
	if m.directory != filepath.Join(node, "runtime") {
		t.Fatal("installer root escaped native node slot", m.directory)
	}
	if _, e = New(m.directory); e == nil {
		t.Fatal("remote installer expanded outside HOME")
	}
	if _, e = os.Stat(m.directory); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("passive constructor created runtime installation", e)
	}
	releases := m.ReviewedReleases()
	if len(releases) == 0 {
		t.Fatal("supported native node has no reviewed releases")
	}
	for _, r := range releases {
		if r.Arch != runtime.GOARCH {
			t.Fatal("wrong architecture", r)
		}
		if runtime.GOOS == "darwin" && (!strings.Contains(r.URL, "_darwin_") || r.Runtime != "caelis") {
			t.Fatal("Darwin selected remote Linux release", r)
		}
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if e = os.Symlink(node, alias); e != nil {
		t.Fatal(e)
	}
	if _, e = NewLocalNode(alias); e == nil {
		t.Fatal("redirected local native root accepted")
	}
	if e = os.Chmod(node, 0755); e != nil {
		t.Fatal(e)
	}
	if _, e = NewLocalNode(node); e == nil {
		t.Fatal("shared local root adopted")
	}
}

func TestLocalNodeReviewedInstallUsesExistingAtomicReceiptAndOriginalID(t *testing.T) {
	m, _ := fixtureManager(t)
	installed := install(t, m, "caelis", "1.0.0", "original-native-install")
	binary, e := m.BinaryPath("caelis")
	if e != nil || binary == "" || !installed.Installed {
		t.Fatal(installed, e)
	}
	repeated, e := m.Manage(t.Context(), Request{Action: "install", Runtime: "caelis", Version: "1.0.0", RequestID: "original-native-install"})
	if e != nil || repeated != installed {
		t.Fatal("original local receipt replayed", repeated, e)
	}
	if _, e = m.Manage(t.Context(), Request{Action: "install", Runtime: "caelis", Version: "2.0.0", RequestID: "original-native-install"}); e == nil {
		t.Fatal("different input reused original receipt")
	}
}
