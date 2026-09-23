package contentpack

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPack(t *testing.T) *Pack {
	t.Helper()
	model, e := os.ReadFile("testdata/basic.glb")
	if e != nil {
		t.Fatal(e)
	}
	var avatar bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			img.Set(x, y, color.NRGBA{40, 180, 200, 255})
		}
	}
	if e = png.Encode(&avatar, img); e != nil {
		t.Fatal(e)
	}
	p := &Pack{Manifest: Manifest{Format: "caelis-content", FormatVersion: 1, ID: "example.orbit", Version: "1.0.0", Name: "Orbit", Author: "Test creator", License: "Apache-2.0", LicenseFile: "LICENSE.txt", Characters: []Character{{ID: "orbit", Name: "Orbit", Variants: []Variant{{ID: "day", Name: "Day", Model: "model.glb", Avatar: "portrait", Capability: "basic-3d-v1"}}}}, Avatars: []Avatar{{ID: "portrait", Name: "Portrait", Image: "avatar.png", Capability: "png-v1"}}}, Files: map[string][]byte{"model.glb": model, "avatar.png": avatar.Bytes(), "LICENSE.txt": []byte("Apache-2.0 test fixture\n")}}
	for _, name := range []string{"LICENSE.txt", "model.glb", "avatar.png"} {
		p.Manifest.Files = append(p.Manifest.Files, File{name, Hash(p.Files[name]), int64(len(p.Files[name]))})
	}
	return p
}
func archiveFile(t *testing.T, p *Pack) string {
	t.Helper()
	b, e := Encode(p)
	if e != nil {
		t.Fatal(e)
	}
	name := filepath.Join(t.TempDir(), "test.caelispack")
	if e = WriteArchive(name, b); e != nil {
		t.Fatal(e)
	}
	return name
}
func rawArchive(t *testing.T, p *Pack, extra string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	m, _ := json.Marshal(p.Manifest)
	w, _ := z.Create("manifest.json")
	w.Write(m)
	for name, data := range p.Files {
		w, _ := z.Create(name)
		w.Write(data)
	}
	if extra != "" {
		w, _ := z.Create(extra)
		w.Write([]byte("unexpected"))
	}
	z.Close()
	return b.Bytes()
}
func TestArchiveRoundTripAndCreatorWorkflow(t *testing.T) {
	p := testPack(t)
	b, e := Encode(p)
	if e != nil {
		t.Fatal(e)
	}
	again, e := Encode(p)
	if e != nil || !bytes.Equal(b, again) {
		t.Fatal("non-deterministic archive")
	}
	got, e := ReadArchive(b)
	if e != nil || got.Manifest.ID != p.Manifest.ID {
		t.Fatal(e)
	}
	dir := filepath.Join(t.TempDir(), "source")
	if e = WriteSource(dir, p); e != nil {
		t.Fatal(e)
	}
	if e = WriteSource(dir, p); e == nil {
		t.Fatal("overwrote creator directory")
	}
	os.WriteFile(filepath.Join(dir, "LICENSE.txt"), []byte("Apache-2.0 new attribution"), 0600)
	updated, e := ReadDirectory(dir)
	if e != nil || updated.Manifest.Files[0].SHA256 == p.Manifest.Files[0].SHA256 {
		t.Fatal("receipt not regenerated", e)
	}
	os.WriteFile(filepath.Join(dir, "unexpected.js"), []byte("code"), 0600)
	if _, e = ReadDirectory(dir); e == nil {
		t.Fatal("accepted unlisted source")
	}
}
func TestRejectUntrustedContent(t *testing.T) {
	for _, extra := range []string{"../escape", "/absolute", "avatar.png", "AVATAR.png", "script.js", "folder/../../escape", "C:/file", "CON.txt"} {
		t.Run(extra, func(t *testing.T) {
			if _, e := ReadArchive(rawArchive(t, testPack(t), extra)); e == nil {
				t.Fatal("accepted", extra)
			}
		})
	}
	p := testPack(t)
	p.Files["model.glb"][0] = 'x'
	if _, e := ReadArchive(rawArchive(t, p, "")); e == nil {
		t.Fatal("accepted hash mismatch")
	}
	p = testPack(t)
	p.Manifest.Characters[0].Variants[0].Capability = "execute-js-v1"
	if _, e := ReadArchive(rawArchive(t, p, "")); e == nil {
		t.Fatal("accepted required capability")
	}
	var m Manifest
	if e := StrictJSON([]byte(`{"format":"caelis-content","format":"evil"}`), &m); e == nil {
		t.Fatal("duplicate key")
	}
	if e := StrictJSON([]byte(`{"unrecognized":true}`), &m); e == nil {
		t.Fatal("unknown manifest property")
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "model.glb"}
	h.SetMode(os.ModeSymlink | 0777)
	w, _ := z.CreateHeader(h)
	w.Write([]byte("/etc/passwd"))
	z.Close()
	if _, e := ReadArchive(buf.Bytes()); e == nil {
		t.Fatal("accepted symlink")
	}
}
func TestRegistryLifecycleAndFences(t *testing.T) {
	root := t.TempDir()
	r, e := NewRegistry(root)
	if e != nil {
		t.Fatal(e)
	}
	p := testPack(t)
	file := archiveFile(t, p)
	s, e := r.Install(file)
	if e != nil || len(s.Packs) != 1 {
		t.Fatal(e)
	}
	key := packageKey(p.Manifest)
	sel := Selection{characterKey(key, "orbit", "day"), "follow"}
	s, e = r.Select(sel)
	if e != nil || s.Appearance.Model == "" || s.Appearance.Avatar == "" {
		t.Fatal(e)
	}
	old := s.Appearance.Revision
	s, e = r.Select(Selection{"builtin:stick", avatarKey(key, "portrait")})
	if e != nil {
		t.Fatal(e)
	}
	s, e = r.Fallback(old)
	if e != nil || s.Appearance.Selection.Avatar == "follow" {
		t.Fatal("stale failure replaced newer choice")
	}
	restored, e := NewRegistry(root)
	if e != nil || restored.State().Appearance.Selection != s.Appearance.Selection {
		t.Fatal("selection not durable", e)
	}
	if _, e = r.Remove(key); e == nil {
		t.Fatal("removed in-use avatar")
	}
	p.Manifest.Name = "Changed bytes, same release"
	if _, e = r.Install(archiveFile(t, p)); e == nil {
		t.Fatal("overwrote immutable version")
	}
	p.Manifest.Version = "1.0.1"
	if _, e = r.Install(archiveFile(t, p)); e != nil {
		t.Fatal(e)
	}
	if _, e = r.Select(sel); e != nil {
		t.Fatal("old version not selectable", e)
	}
	if _, e = r.Select(defaultSelection()); e != nil {
		t.Fatal(e)
	}
	if _, e = r.Remove(key); e != nil {
		t.Fatal(e)
	}
	// Broken installed files cannot alter state or become a startup failure.
	for _, en := range r.entries {
		os.WriteFile(filepath.Join(root, en.Digest+".caelispack"), []byte("damaged"), 0600)
	}
	restored, e = NewRegistry(root)
	if e != nil || len(restored.State().Packs) != 0 {
		t.Fatal("corrupt pack prevented fallback", e)
	}
}
func TestResourceHandlerIsolation(t *testing.T) {
	r, _ := NewRegistry(t.TempDir())
	p := testPack(t)
	s, e := r.Install(archiveFile(t, p))
	if e != nil {
		t.Fatal(e)
	}
	s, e = r.Select(Selection{s.Characters[2].ID, "follow"})
	if e != nil {
		t.Fatal(e)
	}
	handler := r.Handler(http.NotFoundHandler())
	for _, tc := range []struct {
		url    string
		status int
		mime   string
	}{{s.Appearance.Model, 200, "model/gltf-binary"}, {s.Appearance.Avatar, 200, "image/png"}, {strings.Replace(s.Appearance.Model, "model.glb", "LICENSE.txt", 1), 404, ""}, {"/content/unknown/model.glb", 404, ""}, {"/content/unknown/../../secret", 404, ""}} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", tc.url, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.url, w.Code)
		}
		if tc.mime != "" && w.Header().Get("Content-Type") != tc.mime {
			t.Fatal("unsafe MIME")
		}
	}
}
func mutateGLB(t *testing.T, b []byte, change func(map[string]any)) []byte {
	t.Helper()
	n := int(binary.LittleEndian.Uint32(b[12:]))
	var d map[string]any
	if e := json.Unmarshal(b[20:20+n], &d); e != nil {
		t.Fatal(e)
	}
	change(d)
	j, _ := json.Marshal(d)
	for len(j)%4 != 0 {
		j = append(j, ' ')
	}
	out := append([]byte(nil), b[:20]...)
	binary.LittleEndian.PutUint32(out[12:], uint32(len(j)))
	out = append(out, j...)
	out = append(out, b[20+n:]...)
	binary.LittleEndian.PutUint32(out[8:], uint32(len(out)))
	return out
}
func TestGLBTrustAndBudget(t *testing.T) {
	b := testPack(t).Files["model.glb"]
	if e := validateGLB(b); e != nil {
		t.Fatal(e)
	}
	cases := []func(map[string]any){func(d map[string]any) { d["buffers"].([]any)[0].(map[string]any)["uri"] = "file:///secret" }, func(d map[string]any) { d["extensionsRequired"] = []string{"KHR_draco_mesh_compression"} }, func(d map[string]any) { d["nodes"].([]any)[0].(map[string]any)["children"] = []int{0} }, func(d map[string]any) { d["accessors"].([]any)[0].(map[string]any)["count"] = 1e9 }, func(d map[string]any) { d["bufferViews"].([]any)[0].(map[string]any)["byteLength"] = 1e12 }}
	for i, change := range cases {
		if e := validateGLB(mutateGLB(t, b, change)); e == nil {
			t.Fatalf("accepted case %d", i)
		}
	}
}

func TestImageRejectsAnimationAndTruncatedPixels(t *testing.T) {
	b := testPack(t).Files["avatar.png"]
	if err := validateImage(b, "png"); err != nil {
		t.Fatal(err)
	}
	// Keep a valid IHDR, so header-only inspection would incorrectly accept it.
	if err := validateImage(b[:33], "png"); err == nil {
		t.Fatal("accepted missing pixels")
	}
	chunk := make([]byte, 20)
	binary.BigEndian.PutUint32(chunk, 8)
	copy(chunk[4:], "acTL")
	animated := append(append(append([]byte{}, b[:33]...), chunk...), b[33:]...)
	if err := validateImage(animated, "png"); err == nil || !strings.Contains(err.Error(), "animated PNG") {
		t.Fatal("accepted animation", err)
	}
}

func TestReimportRepairsStoredBytesAndFallbackSurvivesSaveFailure(t *testing.T) {
	root := t.TempDir()
	r, err := NewRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	p := testPack(t)
	source := archiveFile(t, p)
	if _, err = r.Install(source); err != nil {
		t.Fatal(err)
	}
	entry := r.entries[packageKey(p.Manifest)]
	stored := filepath.Join(root, entry.Digest+".caelispack")
	if err = os.WriteFile(stored, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Install(source); err != nil {
		t.Fatal(err)
	}
	if _, b, err := ReadFile(stored); err != nil || Hash(b) != entry.Digest {
		t.Fatal("not repaired", err)
	}
	s, err := r.Select(Selection{characterKey(packageKey(p.Manifest), "orbit", "day"), "follow"})
	if err != nil {
		t.Fatal(err)
	}
	// A directory at the persisted selection path makes rename fail even as root.
	if err = os.Remove(filepath.Join(root, "selection.json")); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(root, "selection.json"), 0700); err != nil {
		t.Fatal(err)
	}
	fallback, err := r.Fallback(s.Appearance.Revision)
	if err != nil || fallback.Appearance.Selection != defaultSelection() || fallback.Appearance.Revision <= s.Appearance.Revision || !strings.Contains(fallback.Notice, "无法保存") {
		t.Fatal("failed to recover in memory", fallback, err)
	}
}
