package screeninput

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestMediaSurvivesExportCleanupAndRestart(t *testing.T) {
	root, r := fixture(t, true)
	files, err := Files(root, r)
	if err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(t.TempDir(), "ScreenMedia")
	m, err := OpenMedia(store)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Save("screen-persist", files); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(files[0].Path)
	if err = os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	m, err = OpenMedia(store)
	if err != nil {
		t.Fatal(err)
	}
	images := m.Images("screen-persist")
	if len(images) != 2 || images[0].Role != "selection" || images[1].Role != "context" {
		t.Fatal(images)
	}
	data, mime, err := m.Bytes(images[0].ID, false)
	if err != nil || mime != "image/png" || !bytes.Equal(data, original) {
		t.Fatal("original not retained", err)
	}
	for _, image := range images {
		if data, mime, err = m.Bytes(image.ID, true); err != nil || mime != "image/jpeg" || len(data) == 0 {
			t.Fatal("thumbnail missing", err)
		}
	}
	if err = m.Save("screen-persist", files); err == nil {
		t.Fatal("immutable media overwritten")
	}
	for _, id := range []string{"../capture.png", images[0].ID + "/../../other", mediaKey("screen-persist") + "-2"} {
		if _, _, err = m.Bytes(id, false); err == nil {
			t.Fatal("invalid ID accepted", id)
		}
	}
}

func TestMediaAtomicFailureAndRootConfinement(t *testing.T) {
	root, r := fixture(t, false)
	files, _ := Files(root, r)
	m, _ := OpenMedia(t.TempDir())
	bad := append(append([]api.InputFile{}, files...), api.InputFile{Path: filepath.Join(root, "missing")})
	if err := m.Save("screen-failed", bad); err == nil || len(m.Images("screen-failed")) != 0 {
		t.Fatal("partial save became visible")
	}
	if err := m.Save("screen-failed", files); err != nil {
		t.Fatal("partial save prevented recovery", err)
	}
	id := m.Images("screen-failed")[0].ID
	path := filepath.Join(m.root, mediaKey("screen-failed"), "0")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(files[0].Path, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Bytes(id, false); err == nil {
		t.Fatal("media symlink escaped root")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := m.DataURL(id, false); err == nil {
		t.Fatal("missing bytes presented as valid image")
	}
}

func TestMediaCleanupOnlyExpiresOwnedRecords(t *testing.T) {
	root, r := fixture(t, false)
	files, _ := Files(root, r)
	m, _ := OpenMedia(t.TempDir())
	for _, id := range []string{"screen-old", "screen-new"} {
		if err := m.Save(id, files); err != nil {
			t.Fatal(err)
		}
	}
	key := mediaKey("screen-old")
	record, _ := m.record(key)
	record.Created = time.Now().Add(-31 * 24 * time.Hour)
	data, _ := json.Marshal(record)
	if err := os.WriteFile(filepath.Join(m.root, key, "media.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := m.Storage(false, nil)
	if err != nil || before.Files != 6 || before.EligibleFiles != 3 || !before.CanClean {
		t.Fatal(before, err)
	}
	var trashed []string
	_, err = m.Storage(true, func(path string) error { trashed = append(trashed, path); return os.RemoveAll(path) })
	if err != nil || len(trashed) != 1 || trashed[0] != filepath.Join(m.root, key) {
		t.Fatal(trashed, err)
	}
	if len(m.Images("screen-old")) != 0 || len(m.Images("screen-new")) != 1 {
		t.Fatal("cleanup lost current media")
	}
}
