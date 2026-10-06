package backend

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func testMessagePNG(t *testing.T, name string) api.InputFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 16, 12))); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return api.InputFile{Name: name, Path: path}
}

func TestOrdinaryImagePresentationFollowsReceiptAndRestoresByRequest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "MessageMedia")
	for _, outcome := range []string{"accepted", "rejected", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			file1, file2 := testMessagePNG(t, "first.png"), testMessagePNG(t, "second.png")
			plain := filepath.Join(t.TempDir(), "notes.txt")
			if err := os.WriteFile(plain, []byte("ordinary file"), 0600); err != nil {
				t.Fatal(err)
			}
			files := []api.InputFile{file1, {Name: "notes.txt", Path: plain}, file2}
			e := &delayedInput{entered: make(chan struct{}), release: make(chan struct{}), outcome: outcome}
			s := NewService(e, nil, nil, nil, nil)
			if err := ConfigureMessageMedia(s, root); err != nil {
				t.Fatal(err)
			}
			id := "request-" + outcome
			done := make(chan api.Receipt, 1)
			go func() {
				r, _ := SubmitRemote(context.Background(), s, api.Submission{ID: id, Text: "caption"}, files)
				done <- r
			}()
			<-e.entered
			pending := s.Snapshot().Items
			if len(pending) != 1 || pending[0].Status != "sending" || pending[0].Media == nil || pending[0].Media.Caption != "caption" || len(pending[0].Media.Images) != 2 || pending[0].Screen != nil || !strings.Contains(pending[0].Text, "notes.txt") {
				t.Fatalf("sending projection: %+v", pending)
			}
			crashView := NewService(e, nil, nil, nil, nil)
			if err := ConfigureMessageMedia(crashView, root); err != nil {
				t.Fatal(err)
			}
			if got := crashView.Snapshot().Items; len(got) != 1 || got[0].Status != "unknown" || got[0].RequestID != id {
				t.Fatalf("restart replayed pending send: %+v", got)
			}
			close(e.release)
			if r := <-done; r.Outcome != outcome {
				t.Fatal(r)
			}
			if got := s.Snapshot().Items[0]; got.Status != outcome || got.RequestID != id || len(got.Media.Images) != 2 {
				t.Fatalf("receipt projection: %+v", got)
			}
			for _, f := range files {
				_ = os.Remove(f.Path)
			}
			recoveredPending := NewService(e, nil, nil, nil, nil)
			if err := ConfigureMessageMedia(recoveredPending, root); err != nil {
				t.Fatal(err)
			}
			pendingAfterRestart := recoveredPending.Snapshot().Items
			if outcome == "unknown" {
				if len(pendingAfterRestart) != 1 || pendingAfterRestart[0].Status != outcome || pendingAfterRestart[0].RequestID != id || pendingAfterRestart[0].Media.Caption != "caption" || len(pendingAfterRestart[0].Media.Images) != 2 {
					t.Fatalf("unknown original receipt lost on restart: %+v", pendingAfterRestart)
				}
			} else if len(pendingAfterRestart) != 0 {
				t.Fatalf("terminal receipt resurfaced as fresh input: %+v", pendingAfterRestart)
			}
			e.mu.Lock()
			e.view.Items = []api.Item{{ID: "native-" + id, Kind: "user", RequestID: id, Text: "caption", Status: "completed"}}
			e.mu.Unlock()
			restored := NewService(e, nil, nil, nil, nil)
			if err := ConfigureMessageMedia(restored, root); err != nil {
				t.Fatal(err)
			}
			item := restored.Snapshot().Items[0]
			if item.Media == nil || item.Media.Caption != "caption" || len(item.Media.Images) != 2 || item.Screen != nil || item.Text != "caption" {
				t.Fatalf("history lost: %+v", item)
			}
			for _, img := range item.Media.Images {
				if img.Unavailable || img.Name == "notes.txt" {
					t.Fatal(img)
				}
				url, err := restored.MediaImage(img.ID, false)
				if err != nil || !strings.HasPrefix(url, "data:image/png;base64,") {
					t.Fatal(err)
				}
			}
			if thumb, err := restored.MediaImage(item.Media.Images[0].ID, true); err != nil || !strings.HasPrefix(thumb, "data:image/jpeg;base64,") {
				t.Fatal("thumbnail unavailable", err)
			}
			if _, err := restored.MediaImage("/etc/passwd", false); err == nil {
				t.Fatal("renderer addressed local path")
			}
		})
	}
}

func TestOrdinaryImageUnavailableDoesNotMasqueradeAsCapture(t *testing.T) {
	root := filepath.Join(t.TempDir(), "MessageMedia")
	file := filepath.Join(t.TempDir(), "broken.jpg")
	if err := os.WriteFile(file, []byte("invalid image"), 0600); err != nil {
		t.Fatal(err)
	}
	e := &screenEngine{outcome: "accepted"}
	s := NewService(e, nil, nil, nil, nil)
	if err := ConfigureMessageMedia(s, root); err != nil {
		t.Fatal(err)
	}
	if r, err := SubmitRemote(t.Context(), s, api.Submission{ID: "telegram:1:2", Text: "caption"}, []api.InputFile{{Name: "broken.jpg", Path: file}}); err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	view := s.Snapshot().Items[0]
	if view.Media == nil || len(view.Media.Images) != 1 || !view.Media.Images[0].Unavailable || view.Screen != nil {
		t.Fatal(view)
	}
	if _, err := s.MediaImage(view.Media.Images[0].ID, false); err == nil {
		t.Fatal("invalid bytes exposed")
	}
}
