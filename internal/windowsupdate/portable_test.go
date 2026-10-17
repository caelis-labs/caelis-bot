package windowsupdate

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func portableFixture(t *testing.T, root, version string, extra map[string][]byte) {
	t.Helper()
	files := map[string][]byte{"caelis-bot.exe": []byte("executable-" + version), "build.json": []byte(`{"version":"` + version + `"}`)}
	for name, b := range extra {
		files[name] = b
	}
	p := Portable{Schema: 1, Identity: "dev.caelis.bot.preview", Version: version}
	for name, b := range files {
		if e := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(root, name), b, 0600); e != nil {
			t.Fatal(e)
		}
		p.Files = append(p.Files, File{name, Digest(b), int64(len(b))})
	}
	data, _ := json.Marshal(p)
	if e := os.WriteFile(filepath.Join(root, "portable.json"), data, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestPortableUpdatePreservesForeignFilesAndRefusesChangedOwnedFiles(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "ZIP")
	stage := filepath.Join(base, "new")
	os.Mkdir(target, 0700)
	os.Mkdir(stage, 0700)
	portableFixture(t, target, "1.0.0", map[string][]byte{"Resources/obsolete.txt": []byte("old")})
	portableFixture(t, stage, "1.0.1", nil)
	canary := filepath.Join(target, "user-note.txt")
	os.WriteFile(canary, []byte("keep"), 0600)
	if e := ReplacePortable(target, stage, filepath.Join(base, "backup"), "dev.caelis.bot.preview", "1.0.1"); e != nil {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(canary); string(b) != "keep" {
		t.Fatal("foreign data changed")
	}
	if _, e := os.Stat(filepath.Join(target, "Resources/obsolete.txt")); !os.IsNotExist(e) {
		t.Fatal("obsolete owned file remained")
	}
	if p, e := readPortable(filepath.Join(target, "portable.json")); e != nil || p.Version != "1.0.1" {
		t.Fatal("new inventory absent", e)
	}
	stage = filepath.Join(base, "next")
	os.Mkdir(stage, 0700)
	portableFixture(t, stage, "1.0.2", nil)
	os.WriteFile(filepath.Join(target, "caelis-bot.exe"), []byte("user modification"), 0600)
	if e := ReplacePortable(target, stage, filepath.Join(base, "backup2"), "dev.caelis.bot.preview", "1.0.2"); e == nil {
		t.Fatal("changed installed file silently replaced")
	}
}
func TestZIPRejectsTraversalDevicesAndCaseAliases(t *testing.T) {
	for _, name := range []string{"../canary", "/absolute", "C:/canary", "Resources\\foo", "Resources/CON.txt", "file. ", "nested/../escape"} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			z := zip.NewWriter(&b)
			f, _ := z.Create(name)
			f.Write([]byte("bad"))
			z.Close()
			path := filepath.Join(t.TempDir(), "bad.zip")
			os.WriteFile(path, b.Bytes(), 0600)
			if _, e := Unpack(path, filepath.Join(filepath.Dir(path), "out"), "dev.caelis.bot.preview", "1.0.1"); e == nil {
				t.Fatal("unsafe ZIP accepted")
			}
		})
	}
	base := t.TempDir()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, name := range []string{"FILE.txt", "file.txt"} {
		f, _ := z.Create(name)
		f.Write([]byte("bad"))
	}
	z.Close()
	path := filepath.Join(base, "bad.zip")
	os.WriteFile(path, b.Bytes(), 0600)
	if _, e := Unpack(path, filepath.Join(base, "out"), "dev.caelis.bot.preview", "1.0.1"); e == nil {
		t.Fatal("case alias accepted")
	}
}
