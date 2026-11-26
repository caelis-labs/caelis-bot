package desktop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type localChatTestEngine struct{ api.Engine }

func (localChatTestEngine) Snapshot() api.Snapshot      { return api.Snapshot{} }
func (localChatTestEngine) Close(context.Context) error { return nil }

func TestLocalChatFileDelivery(t *testing.T) {
	root := t.TempDir()
	evidence := os.Getenv("BOT_CHAT_FILE_EVIDENCE_DIR")
	actualEvidence := evidence != ""
	if !actualEvidence {
		evidence = filepath.Join(root, "Worker 截图")
		if err := os.Mkdir(evidence, 0700); err != nil {
			t.Fatal(err)
		}
	}
	labels := []string{"亮色", "暗色", "大图预览", "已提交消息与保留的草稿"}
	names := []string{"combined-light.png", "combined-narrow-dark.png", "combined-viewer.png", "combined-submitted.png"}
	var original []string
	originalBytes := make(map[string][]byte)
	for i, name := range names {
		path := filepath.Join(evidence, name)
		if !actualEvidence {
			writeLocalChatTestPNG(t, path, 30+i)
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		originalBytes[name] = source
		original = append(original, markdownFile(labels[i], path))
	}
	projectRoot := root
	output := os.Getenv("BOT_CHAT_FILE_CAPTURE_INPUT")
	if output != "" {
		projectRoot = filepath.Dir(output)
	}
	project, err := os.MkdirTemp(projectRoot, "项目 目录-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(project) })
	imagePath := filepath.Join(project, "设计 图.png")
	filePath := filepath.Join(project, "交付 说明.txt")
	writeLocalChatTestPNG(t, imagePath, 360)
	if err := os.WriteFile(filePath, []byte("Project delivery check\n"), 0600); err != nil {
		t.Fatal(err)
	}
	originalBytes[filepath.Base(imagePath)], err = os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	first := api.Item{ID: "worker-screenshots", Kind: "assistant", Status: "completed", Text: strings.Join(original, "\n\n")}
	projectMessage := []string{
		markdownFile("项目图片", imagePath) + " · " + markdownFile("同一图片", imagePath),
		markdownFile("项目说明", filePath),
		markdownFile("已移除的图片", filepath.Join(project, "失效 图片.png")),
		markdownFile("目录", project),
		"`" + markdownFile("代码中的链接", filePath) + "`",
		"[远程图片](https://example.com/picture.png)",
	}
	second := api.Item{ID: "project-files", Kind: "assistant", Status: "completed", Text: strings.Join(projectMessage, "\n\n")}
	input := api.Snapshot{Items: []api.Item{first, second}}
	chat := backend.NewService(localChatTestEngine{}, nil, nil, nil, nil)
	chat.ConfigureChat(filepath.Join(root, "chat.sqlite"))
	t.Cleanup(func() { _ = chat.Shutdown() })
	chat.ObserveChat(input)
	desktop := &Service{}
	desktop.localChat.snapshot = chat.ChatSnapshot

	view := desktop.LocalChatSnapshot(0, "", "").Snapshot
	if len(view.Items) != 2 || view.Items[0].Text != first.Text || view.Items[1].Text != second.Text ||
		len(view.Items[0].Links) != 4 || len(view.Items[1].Links) != 4 || len(desktop.localChat.files) != 6 {
		t.Fatal("local chat changed the original text or duplicated its file handles")
	}
	if len(view.Items[0].Artifacts) != 0 || len(view.Items[1].Artifacts) != 0 {
		t.Fatal("inline links were also rendered as attachments")
	}
	for _, item := range chat.ChatSnapshot(0, "", "").Snapshot.Items {
		if len(item.Links) != 0 || len(item.Artifacts) != 0 {
			t.Fatal("local handles reached the shared Web/channel projection")
		}
	}
	images := map[string][2]string{}
	for _, link := range append(view.Items[0].Links, view.Items[1].Links[0]) {
		if link.Image == nil {
			t.Fatal("local image has no preview handle")
		}
		want := originalBytes[link.File.Name]
		id := link.Image.ID
		thumb, err := desktop.LocalChatImage(id, true)
		if err != nil || !strings.HasPrefix(thumb, "data:image/jpeg;base64,") {
			t.Fatal("native thumbnail unavailable", err)
		}
		full, err := desktop.LocalChatImage(id, false)
		if err != nil || full != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(want) {
			t.Fatal("full preview did not read the original PNG", err)
		}
		images[id] = [2]string{thumb, full}
		if _, err := chat.MediaImage(id, false); err == nil {
			t.Fatal("shared MediaImage opened a native local handle")
		}
		if _, err := backend.ResolveRemoteArtifact(chat, id); err == nil {
			t.Fatal("channel export opened a native local handle")
		}
	}
	file := view.Items[1].Links[1].File
	var opened []string
	desktop.openArtifactFile = func(path string) error {
		opened = append(opened, path)
		if os.Getenv("BOT_CHAT_FILE_OPEN_PROJECT") == "1" {
			return exec.Command("/usr/bin/open", "-g", path).Run()
		}
		return nil
	}
	realFile, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := desktop.OpenLocalChatFile(file.ID); err != nil || len(opened) != 1 || opened[0] != realFile {
		t.Fatal("local document did not reach native open with its original path", err)
	}
	if err := desktop.OpenLocalChatFile(filePath); err == nil || len(opened) != 1 {
		t.Fatal("renderer passed a raw path to native open")
	}
	if links := view.Items[1].Links; links[2].File.ID != "" || links[3].File.ID != "" {
		t.Fatal("missing file or directory acquired a handle")
	}
	if err := os.Remove(imagePath); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.LocalChatImage(view.Items[1].Links[0].Image.ID, false); err == nil {
		t.Fatal("removed file could still be read")
	}
	reopened := desktop.LocalChatSnapshot(0, "", "").Snapshot
	if reopened.Items[0].Links[0].Image.ID != view.Items[0].Links[0].Image.ID || reopened.Items[1].Links[0].File.ID != "" || len(reopened.Items) != 2 {
		t.Fatal("reopening duplicated the message or restored an unavailable link")
	}
	verifyLocalChatHistoryRestart(t, chat, root, first.Text)
	if output != "" {
		data, err := json.Marshal(map[string]any{"items": view.Items, "images": images, "labels": labels, "file": file.ID})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLocalChatReplacesAnAbsoluteCitationInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "设计 图.png")
	writeLocalChatTestPNG(t, path, 36)
	destination := filepath.ToSlash(path)
	uri := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(destination, "/")}).String()
	item := api.Item{Kind: "assistant", Status: "completed", Text: markdownFile("原标签", path) + " [文件 URI](" + uri + ")",
		Links: []api.ChatLink{{Destination: destination, File: api.Artifact{ID: "legacy", Name: "设计 图.png"}}}}
	files := map[string]localChatFile{}
	projectLocalChatFiles(&item, files)
	if len(item.Links) != 2 || len(files) != 1 || item.Links[0].Image == nil || item.Links[1].Image == nil ||
		item.Links[0].Image.ID != item.Links[1].Image.ID || item.Links[0].Image.ID == "legacy" {
		t.Fatal("absolute link or URI did not share the original desktop preview")
	}
}

func writeLocalChatTestPNG(t *testing.T, path string, width int) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	picture := image.NewRGBA(image.Rect(0, 0, width, 90))
	draw.Draw(picture, picture.Bounds(), image.NewUniform(color.RGBA{R: 21, G: 69, B: 77, A: 255}), image.Point{}, draw.Src)
	draw.Draw(picture, image.Rect(width/8, 20, 7*width/8, 30), image.NewUniform(color.RGBA{R: 185, G: 236, B: 211, A: 255}), image.Point{}, draw.Src)
	if err := png.Encode(file, picture); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func markdownFile(label, path string) string {
	destination := filepath.ToSlash(path)
	if strings.ContainsAny(destination, " ()") {
		destination = "<" + destination + ">"
	}
	return "[" + label + "](" + destination + ")"
}

func verifyLocalChatHistoryRestart(t *testing.T, chat *backend.Service, root, text string) {
	t.Helper()
	if err := chat.Shutdown(); err != nil {
		t.Fatal(err)
	}
	restored := backend.NewService(localChatTestEngine{}, nil, nil, nil, nil)
	restored.ConfigureChat(filepath.Join(root, "chat.sqlite"))
	t.Cleanup(func() { _ = restored.Shutdown() })
	desktop := &Service{}
	desktop.localChat.snapshot = restored.ChatSnapshot
	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot := desktop.LocalChatSnapshot(0, "", "").Snapshot
		if len(snapshot.Items) != 0 {
			if len(snapshot.Items) != 2 || snapshot.Items[0].Text != text || len(snapshot.Items[0].Links) != 4 || snapshot.Items[1].Links[0].File.ID != "" {
				t.Fatal("restarted desktop did not derive the original links from persisted IM")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("persisted IM never loaded")
		}
		time.Sleep(time.Millisecond)
	}
}
