package messageimage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRetainedWebPUsesOpaqueReadAfterSourceRemoval(t *testing.T) {
	original, err := os.ReadFile("../../frontend/public/portraits/caelis-sage-v1/focus.webp")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sticker.webp")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(t.TempDir(), "MessageMedia"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("telegram:123:7", "Sticker caption", []api.InputFile{{Name: "sticker.webp", Path: path}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Mark("telegram:123:7", "caption", "accepted"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	images := s.Images("telegram:123:7")
	if len(images) != 1 || images[0].Unavailable {
		t.Fatal(images)
	}
	url, err := s.DataURL(images[0].ID, false)
	if err != nil || !strings.HasPrefix(url, "data:image/webp;base64,") {
		t.Fatal(err)
	}
	if thumb, err := s.DataURL(images[0].ID, true); err != nil || !strings.HasPrefix(thumb, "data:image/webp;base64,") {
		t.Fatal(err)
	}
	if _, err := s.DataURL(path, false); err == nil {
		t.Fatal("arbitrary renderer path read")
	}
}
