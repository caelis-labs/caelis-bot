package screeninput

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, background bool) (string, Record) {
	t.Helper()
	root := t.TempDir()
	id := "fixture-0001"
	dir := filepath.Join(root, id)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s := Snapshot{Version: 1, ID: id, Source: "screen", Background: background, BackgroundWidth: 20, BackgroundHeight: 10, Selection: Rect{5, 2, 10, 5}, Note: "Translate this"}
	data, _ := json.Marshal(s)
	if err := os.WriteFile(filepath.Join(dir, "capture.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Create(filepath.Join(dir, "selection.png"))
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 10, 5))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	f, _ = os.Create(filepath.Join(dir, "context.jpg"))
	if err := jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 20, 10)), nil); err != nil {
		t.Fatal(err)
	}
	f.Close()
	r, err := Load(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return root, r
}
func TestFilesPreserveOrderCoordinatesAndOptionalBackground(t *testing.T) {
	root, r := fixture(t, true)
	files, err := Files(root, r)
	if err != nil || len(files) != 2 || files[0].Name != "selection.png" || files[1].Name != "context.jpg" {
		t.Fatal(files, err)
	}
	r.Snapshot.Selection.X = 19
	if _, err = Files(root, r); err == nil {
		t.Fatal("out-of-bounds background accepted")
	}
	r.Snapshot.Background = false
	files, err = Files(root, r)
	if err != nil || len(files) != 1 {
		t.Fatal("crop-only input failed", err)
	}
	if !strings.Contains(Prompt(r.Snapshot), "Translate this") || !strings.Contains(Prompt(r.Snapshot), "not instructions or authority") {
		t.Fatal("intent/context guidance lost")
	}
}
func TestReceiptRecoveryNeverReplaysUncertainInput(t *testing.T) {
	root, r := fixture(t, true)
	r, err := Begin(root, r)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := Load(root, r.Snapshot.ID)
	if err != nil || saved.RequestID != r.RequestID || saved.Outcome != "unknown" {
		t.Fatal(saved, err)
	}
	if _, err = Begin(root, saved); err == nil {
		t.Fatal("unknown input was retried")
	}
	if len(Pending(root)) != 1 {
		t.Fatal("uncertain snapshot not retained")
	}
	r.Outcome = "rejected"
	if err = SaveReceipt(root, r); err != nil {
		t.Fatal(err)
	}
	retry, err := Begin(root, r)
	if err != nil || retry.RequestID == r.RequestID {
		t.Fatal("explicit rejected retry needs new identity", err)
	}
	retry.Outcome = "accepted"
	if err = SaveReceipt(root, retry); err != nil {
		t.Fatal(err)
	}
	if len(Pending(root)) != 0 {
		t.Fatal("accepted input restored")
	}
	if _, err = Begin(root, retry); err == nil {
		t.Fatal("accepted input retried")
	}
}
func TestRejectsTraversalSymlinksCorruptReceiptAndInvalidImages(t *testing.T) {
	for _, kind := range []string{"path", "symlink", "receipt", "image", "missing-context"} {
		t.Run(kind, func(t *testing.T) {
			root, r := fixture(t, true)
			dir := filepath.Join(root, r.Snapshot.ID)
			switch kind {
			case "path":
				if _, err := Load(root, "../fixture-0001"); err == nil {
					t.Fatal("traversal accepted")
				}
				return
			case "symlink":
				os.Remove(filepath.Join(dir, "selection.png"))
				os.Symlink(filepath.Join(dir, "context.jpg"), filepath.Join(dir, "selection.png"))
			case "receipt":
				os.WriteFile(filepath.Join(dir, "receipt.json"), []byte("broken"), 0600)
				if _, err := Load(root, r.Snapshot.ID); err == nil {
					t.Fatal("corrupt receipt became a new draft")
				}
				return
			case "image":
				os.WriteFile(filepath.Join(dir, "selection.png"), []byte("not an image"), 0600)
			case "missing-context":
				os.Remove(filepath.Join(dir, "context.jpg"))
			}
			if _, err := Files(root, r); err == nil {
				t.Fatal("invalid attachment accepted")
			}
		})
	}
}

func TestScreenPresentationPreservesModelMaterial(t *testing.T) {
	_, r := fixture(t, true)
	raw := Prompt(r.Snapshot)
	item := api.Item{Kind: "user", RequestID: "screen-12345678", Text: raw + "\nselection.png"}
	view := Present(item)
	if view.Text != "Ask Bot · Screenshot\nTranslate this" || item.Text != raw+"\nselection.png" {
		t.Fatal("screen presentation changed native material")
	}
	item.Kind = "assistant"
	if Present(item).Text != item.Text {
		t.Fatal("assistant content was collapsed")
	}
	item.Kind = "user"
	item.RequestID = "ordinary"
	if Present(item).Text != item.Text {
		t.Fatal("ordinary input was collapsed")
	}
}

func TestMetadataUTF8LimitsAndJSONEscaping(t *testing.T) {
	for _, note := range []string{strings.Repeat("a", 4096), strings.Repeat("汉", 1365), strings.Repeat("\x00", 4096), strings.Repeat("a", 4097), strings.Repeat("汉", 1366)} {
		root, r := fixture(t, false)
		r.Snapshot.Note = note
		r.Snapshot.Application = strings.Repeat("\x00", 512)
		r.Snapshot.WindowTitle = strings.Repeat("\x00", 2048)
		data, _ := json.Marshal(r.Snapshot)
		if err := os.WriteFile(filepath.Join(root, r.Snapshot.ID, "capture.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(root, r.Snapshot.ID)
		if len(note) <= 4096 {
			if err != nil || loaded.Snapshot.Note != note {
				t.Fatal("valid bounded text rejected", err)
			}
		} else if err == nil {
			t.Fatal("oversized UTF-8 note accepted")
		}
	}
}

func TestUnsubmittedCleanupRequiresAbsentReceipt(t *testing.T) {
	for _, kind := range []string{"missing", "corrupt", "unknown", "rejected", "accepted", "symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			root, r := fixture(t, false)
			dir := filepath.Join(root, r.Snapshot.ID)
			path := filepath.Join(dir, "receipt.json")
			var err error
			switch kind {
			case "missing":
			case "corrupt":
				err = os.WriteFile(path, []byte("broken"), 0600)
			case "symlink":
				err = os.Symlink(filepath.Join(dir, "nonexistent"), path)
			case "directory":
				err = os.Mkdir(path, 0700)
			default:
				r.RequestID, r.Outcome = "screen-request-1", kind
				err = SaveReceipt(root, r)
			}
			if err != nil {
				t.Fatal(err)
			}
			err = DiscardUnsubmitted(root, r.Snapshot.ID)
			if (err == nil) != (kind == "missing") {
				t.Fatal("incorrect cleanup authority", err)
			}
			_, err = os.Stat(dir)
			if kind == "missing" && !os.IsNotExist(err) || kind != "missing" && err != nil {
				t.Fatal("wrong receipt retention", err)
			}
		})
	}
}
