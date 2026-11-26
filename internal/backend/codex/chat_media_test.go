package codex

import (
	"encoding/json"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func writeChatMediaPNG(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 18, 12)))
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
}

func TestCodexGeneratedImageInChatAndSQLite(t *testing.T) {
	s, f := sessionPair(t, "")
	home := filepath.Dir(s.opts.StateFile)
	const thread, turn = "thread-native", "image-turn"
	file := filepath.Join(home, "generated_images", thread, "real-shape.png")
	writeChatMediaPNG(t, file)

	chat := backend.NewService(s, nil, nil, nil, nil)
	db := filepath.Join(home, "chat.sqlite")
	media := filepath.Join(home, "media")
	chat.ConfigureChat(db)
	if err := backend.ConfigureMessageMedia(chat, media); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = chat.Shutdown() })
	event := func(method, thread, turn string, item any) {
		f.emit(wireMessage{Method: method, Params: raw(map[string]any{"threadId": thread, "turnId": turn, "item": item})})
	}
	render := func(v api.Snapshot) []api.Item {
		chat.ObserveChat(v)
		return chat.ChatSnapshot(0, "", "").Snapshot.Items
	}
	event("item/completed", thread, turn, nativeItem{ID: "reply", Type: "agentMessage", Text: "Here is the result."})
	view := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Items) == 1 })
	if got := render(view); len(got) != 1 || got[0].Kind != "assistant" {
		t.Fatal(got)
	}
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": thread, "turn": nativeTurn{ID: turn, Status: "completed"}})})
	_ = awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" })
	// Native ImageGen carries a sizable string result. Only savedPath is consumed.
	wireImage := map[string]any{"type": "imageGeneration", "id": "output", "status": "completed", "savedPath": file, "result": strings.Repeat("a", 9<<20)}
	event("item/completed", thread, turn, wireImage)
	view = awaitState(t, s, func(v api.Snapshot) bool { return len(v.Items) == 2 && v.Items[1].ImageOutput != nil })
	got := render(view)
	if len(got) != 2 || got[0].Media == nil || len(got[0].Media.Images) != 1 || got[1].Text != "Here is the result." {
		t.Fatalf("media did not appear at the original result position: %+v", got)
	}
	id := got[0].Media.Images[0].ID
	for _, thumbnail := range []bool{true, false} {
		if data, err := chat.MediaImage(id, thumbnail); err != nil || !strings.HasPrefix(data, "data:image/") {
			t.Fatalf("opaque image lookup (thumbnail=%v): %v", thumbnail, err)
		}
	}
	for _, unowned := range []string{file, "result-" + strings.Repeat("a", 64) + "-0"} {
		if _, err := chat.MediaImage(unowned, false); err == nil {
			t.Fatal("unowned path or handle was readable")
		}
	}
	event("item/completed", thread, turn, wireImage)
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": thread, "turn": nativeTurn{ID: turn, Status: "completed", Items: []nativeItem{
		{ID: "output", Type: "imageGeneration", Status: "completed", SavedPath: file},
		{ID: "reply", Type: "agentMessage", Text: "Here is the result."},
	}}})})
	view = awaitState(t, s, func(v api.Snapshot) bool { return v.Revision >= view.Revision+2 })
	if repeated := render(view); len(repeated) != 2 || repeated[0].Media.Images[0].ID != id {
		t.Fatal("duplicate native history produced different media", repeated)
	}
	if err := chat.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	// Offline restart loads the original chat row and private bytes.
	reopened := backend.NewService(s, nil, nil, nil, nil)
	reopened.ConfigureChat(db)
	t.Cleanup(func() { _ = reopened.Shutdown() })
	if err := backend.ConfigureMessageMedia(reopened, media); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		persisted := reopened.ChatSnapshot(0, "", "").Snapshot.Items
		if len(persisted) == 2 && persisted[0].Media != nil && persisted[0].Media.Images[0].ID == id {
			if _, err := reopened.MediaImage(id, false); err != nil {
				t.Fatal(err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond) // Await the SQLite reader.
	}
	t.Fatal("offline chat did not restore the original generated media")
}

func TestCodexChatLinksStayScopedAndKeepMarkdown(t *testing.T) {
	root := t.TempDir()
	notebook := filepath.Join(root, "Notebook")
	pngPath := filepath.Join(notebook, "设计 图.png")
	writeChatMediaPNG(t, pngPath)
	private := filepath.Join(notebook, ".attachments", "private.png")
	writeChatMediaPNG(t, private)
	outside := filepath.Join(root, "foreign.png")
	writeChatMediaPNG(t, outside)
	for _, alias := range []struct{ name, target string }{{"alias.png", outside}, {"upload.png", private}} {
		if err := os.Symlink(alias.target, filepath.Join(notebook, alias.name)); err != nil {
			t.Fatal(err)
		}
	}
	s := NewSession(SessionOptions{Directory: notebook})
	t.Cleanup(s.cancelLife)
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(pngPath)}).String()
	reply := "Before [original image](<" + pngPath + ">) after.\n\n" +
		"![same label](设计%20图.png) [URI](" + uri + ") [missing](missing.txt) [web](https://example.com)\n\n" +
		"[alias](alias.png) [upload](upload.png) [private](.attachments/private.png) [outside](../foreign.png)\n\n" +
		"`[literal](ignored.md)`"
	s.applyItem("", "turn", nativeItem{ID: "reply", Type: "agentMessage", Text: reply}, true)
	item := s.Snapshot().Items[0]
	if item.ChatText != reply || len(item.Links) != 6 || len(item.Artifacts) != 1 {
		t.Fatal("unexpected Markdown projection", item)
	}
	for i, link := range item.Links {
		if i < 3 {
			if link.Source == nil || link.File.ID == "" {
				t.Fatal("scoped image missing", i)
			}
		} else if link.File.ID != "" || link.Source != nil {
			t.Fatal("unowned or missing file acquired authority", i)
		}
	}
	chat := backend.NewService(s, nil, nil, nil, nil)
	chat.ConfigureChat(filepath.Join(root, "chat.sqlite"))
	t.Cleanup(func() { _ = chat.Shutdown() })
	if err := backend.ConfigureMessageMedia(chat, filepath.Join(root, "media")); err != nil {
		t.Fatal(err)
	}
	chat.ObserveChat(s.Snapshot())
	view := chat.ChatSnapshot(0, "", "").Snapshot.Items[0]
	if view.Text != reply || len(view.Artifacts) != 0 || view.Links[0].Image == nil || view.Links[0].Image.ID != view.Links[1].Image.ID {
		t.Fatal("in-place image and shared retained handle were lost")
	}
	if err := os.Remove(pngPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, pngPath); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Artifact(view.Links[0].File.ID); err == nil {
		t.Fatal("existing citation read a swapped local path")
	}
	encoded, err := json.Marshal(view.Links[0])
	if err != nil || strings.Contains(string(encoded), "Source") {
		t.Fatal("file opener leaked into renderer JSON")
	}
}

func TestCodexImageGenerationRejectsUnscopedFiles(t *testing.T) {
	home := t.TempDir()
	const thread = "resident"
	outputRoot := filepath.Join(home, "generated_images", thread)
	outside := filepath.Join(home, "private.png")
	otherThread := filepath.Join(home, "generated_images", "another-thread", "other.png")
	writeChatMediaPNG(t, outside)
	writeChatMediaPNG(t, otherThread)
	writeChatMediaPNG(t, filepath.Join(outputRoot, "valid.png"))
	symlink := filepath.Join(outputRoot, "link.png")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Fatal(err)
	}
	s := NewSession(SessionOptions{})
	t.Cleanup(s.cancelLife)
	s.binding.ThreadID = thread
	s.client = &Client{home: home}
	for _, file := range []string{outside, otherThread, "valid.png", ""} {
		item := api.Item{}
		s.projectImageOutput(thread, &item, file)
		if item.ImageOutput != nil {
			t.Fatal("unscoped savedPath was accepted")
		}
	}
	item := api.Item{}
	s.projectImageOutput(thread, &item, symlink)
	if item.ImageOutput == nil {
		t.Fatal("expected file validation at read boundary")
	}
	if f, err := item.ImageOutput.Open(); err == nil {
		f.Close()
		t.Fatal("generated-image symlink escaped the scoped file reader")
	}
}

func TestRecentCodexImageRecoveryUsesFullPageWithoutReplayingTurn(t *testing.T) {
	s, f := sessionPair(t, "")
	const thread, turn = "thread-native", "previous-turn"
	file := filepath.Join(filepath.Dir(s.opts.StateFile), "generated_images", thread, "previous.png")
	writeChatMediaPNG(t, file)
	linked := filepath.Join(s.opts.Directory, "Notebook sample.png")
	writeChatMediaPNG(t, linked)
	const oldText = "Before original label after."
	originalText := "Before [original label](<" + linked + ">) after."

	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method != "thread/turns/list" {
			return nil, false
		}
		var p struct {
			ThreadID, ItemsView, SortDirection string
			Limit                              int
		}
		if err := json.Unmarshal(m.Params, &p); err != nil || p.ThreadID != thread || p.ItemsView != "full" || p.SortDirection != "desc" || p.Limit != 5 {
			t.Errorf("unexpected image history read: %+v, %v", p, err)
		}
		return turnPage{Data: []nativeTurn{{ID: turn, Status: "completed", Items: []nativeItem{
			{ID: "older-input", Type: "userMessage", ClientID: "old-request"},
			{ID: "generated", Type: "imageGeneration", Status: "completed", SavedPath: file},
			{ID: "older-reply", Type: "agentMessage", Text: originalText},
			{ID: "unrecorded", Type: "agentMessage", Text: originalText},
		}}}}, true
	}
	f.mu.Unlock()
	s.mu.Lock()
	c, epoch := s.client, s.epoch
	s.mu.Unlock()

	s.observeRecentChatMedia(c, epoch, thread)
	s.observeRecentChatMedia(c, epoch, thread)
	got := s.Snapshot()
	if len(got.Items) != 1 || got.Items[0].ImageOutput == nil || got.Items[0].BeforeItem != opaque(turn, "older-reply") || got.Items[0].ID != opaque(turn, "generated") {
		t.Fatal("the original native result and reply boundary were not restored")
	}
	if len(got.ChatLinkRepairs) != 2 || got.ChatLinkRepairs[0].ChatText != originalText {
		t.Fatal("native original Markdown was not recovered")
	}
	if got.CurrentTurn != "" || got.LastReceipt.ID != "" || !got.CanSend {
		t.Fatal("historical result altered live admission")
	}
	chat := backend.NewService(s, nil, nil, nil, nil)
	root := t.TempDir()
	chat.ConfigureChat(filepath.Join(root, "chat.sqlite"))
	t.Cleanup(func() { _ = chat.Shutdown() })
	if err := backend.ConfigureMessageMedia(chat, filepath.Join(root, "media")); err != nil {
		t.Fatal(err)
	}
	chat.ObserveChat(api.Snapshot{Items: []api.Item{{
		ID: opaque(turn, "older-reply"), TurnKey: opaque(turn), Status: "completed", Kind: "assistant",
		Text: oldText, SeenAt: 1000,
	}}})
	chat.ObserveChat(got)
	chat.ObserveChat(got)
	items := chat.ChatSnapshot(0, "", "").Snapshot.Items
	if len(items) != 2 || items[0].Media == nil || items[1].Text != originalText || items[1].SeenAt != 1000 ||
		len(items[1].Links) != 1 || items[1].Links[0].Image == nil {
		t.Fatal("historical media did not preserve the original IM row and position", items)
	}
}
