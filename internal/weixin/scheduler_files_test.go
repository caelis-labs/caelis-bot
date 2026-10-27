package weixin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func queuedFile(t *testing.T, f *deliveryFixture, id, name string) {
	t.Helper()
	f.mu.Lock()
	f.snapshot.Items = append(f.snapshot.Items, api.Item{ID: "file-" + id, TurnKey: "turn", Kind: "tool", Status: "completed", SeenAt: f.now.UnixMicro() + int64(len(f.snapshot.Items)+1), Artifacts: []api.Artifact{{ID: id, Name: name}}})
	f.mu.Unlock()
}
func ownFiles(t *testing.T, f *deliveryFixture, names ...string) {
	t.Helper()
	paths := map[string]string{}
	for _, id := range names {
		path := filepath.Join(f.root, id)
		if err := os.WriteFile(path, []byte("bytes:"+id), 0600); err != nil {
			t.Fatal(err)
		}
		paths[id] = path
	}
	f.b.host.Artifact = func(id string) (string, error) {
		if path, ok := paths[id]; ok {
			return path, nil
		}
		return "", errors.New("not owned")
	}
}
func fileCounts(server *fileFixture) (int, int) {
	server.mu.Lock()
	defer server.mu.Unlock()
	return len(server.items), server.uploads
}
func waitUpload(t *testing.T, server *fileFixture) {
	t.Helper()
	deadline := time.After(4 * time.Second)
	for {
		_, n := fileCounts(server)
		if n > 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("upload did not start")
		case <-time.After(time.Millisecond):
		}
	}
}
func fileWindow(f *deliveryFixture) {
	f.b.mu.Lock()
	f.b.state.Window.ContextToken = "ctx"
	f.b.state.ContextToken = "ctx"
	f.b.mu.Unlock()
}

func TestDeliveryFilesKeepOwnedHandlesBytesAndDistinctNames(t *testing.T) {
	f := newDeliveryFixture(t)
	server := newFileFixture(t)
	f.p = server.protocol()
	fileWindow(f)
	ownFiles(t, f, "one", "two")
	f.add("reply", "结果已准备好，完整记录保存在桌面。", resultPriority)
	queuedFile(t, f, "one", "result.json")
	queuedFile(t, f, "two", "result.json")
	f.poll()
	for _, id := range []string{"one", "two"} {
		f.advance(ordinaryDelay)
		f.poll()
		if out := waitFile(t, f.b, id); out.State != "accepted" || out.Attempts != 1 {
			t.Fatal(out)
		}
		f.b.fileWait.Wait()
	}
	if n, u := fileCounts(server); n != 3 || u != 2 || f.used() != 3 {
		t.Fatalf("send/upload counts %d/%d used=%d", n, u, f.used())
	}
	server.mu.Lock()
	for _, item := range server.items[1:] {
		if item.File == nil || item.File.Name != "result.json" {
			t.Fatal(item)
		}
	}
	seen := map[string]bool{}
	for _, body := range server.uploaded {
		seen[string(body)] = true
	}
	server.mu.Unlock()
	if !seen["bytes:one"] || !seen["bytes:two"] {
		t.Fatal(seen)
	}
	f.reopen()
	f.poll()
	if n, u := fileCounts(server); n != 3 || u != 2 || f.used() != 3 {
		t.Fatalf("restart replay %d/%d", n, u)
	}
}

func TestDeliveryUploadDoesNotBlockApprovalOrSpendSlot(t *testing.T) {
	f := newDeliveryFixture(t)
	server := newFileFixture(t)
	server.uploadGate = make(chan struct{})
	f.p = server.protocol()
	fileWindow(f)
	ownFiles(t, f, "file")
	queuedFile(t, f, "file", "file.txt")
	f.poll()
	waitUpload(t, server)
	if f.used() != 0 {
		t.Fatal("upload spent send slot")
	}
	f.approval("urgent")
	f.poll()
	if f.used() != 1 {
		t.Fatal("approval stalled behind upload")
	}
	server.mu.Lock()
	if len(server.items) != 1 || !strings.Contains(server.items[0].Text.Text, "/approve") {
		t.Fatal(server.items)
	}
	server.mu.Unlock()
	close(server.uploadGate)
	f.b.fileWait.Wait()
	f.advance(ordinaryDelay)
	f.poll()
	if out := waitFile(t, f.b, "file"); out.State != "accepted" {
		t.Fatal(out)
	}
	if f.used() != 2 {
		t.Fatal("upload or typing counted")
	}
}

func TestDeliveryNewWindowInvalidatesUnsentFileAndRestartResumesPreparation(t *testing.T) {
	f := newDeliveryFixture(t)
	server := newFileFixture(t)
	server.uploadGate = make(chan struct{})
	f.p = server.protocol()
	fileWindow(f)
	ownFiles(t, f, "file")
	queuedFile(t, f, "file", "file.txt")
	f.poll()
	waitUpload(t, server)
	f.window("B")
	fileWindow(f)
	close(server.uploadGate)
	f.b.fileWait.Wait()
	if n, _ := fileCounts(server); n != 0 || f.used() != 0 {
		t.Fatal("old queued file borrowed new window")
	}
	if out := waitFile(t, f.b, "file"); out.Attempts != 0 || out.State != "deferred" {
		t.Fatal(out)
	}
	g := newDeliveryFixture(t)
	other := newFileFixture(t)
	g.p = other.protocol()
	fileWindow(g)
	ownFiles(t, g, "file")
	queuedFile(t, g, "file", "file.txt")
	// Persist an unattempted preparation without exposing a local path. Restart
	// may re-upload it, but only the sendmessage intent reserves a slot.
	g.b.observeDelivery(g.b.host.Snapshot())
	g.b.mu.Lock()
	g.b.state.Outputs[artifactKey("turn", "file")] = outbound{State: "preparing", ArtifactID: "file", FileTurnKey: "turn", FileName: "file.txt", FileInputID: "A"}
	_ = g.b.saveLocked()
	g.b.mu.Unlock()
	g.reopen()
	g.poll()
	if out := waitFile(t, g.b, "file"); out.State != "accepted" || g.used() != 1 {
		t.Fatal(out)
	}
}

func TestDeliveryForeignArtifactNeverUploadsOrConsumesBudget(t *testing.T) {
	f := newDeliveryFixture(t)
	server := newFileFixture(t)
	f.p = server.protocol()
	fileWindow(f)
	ownFiles(t, f)
	queuedFile(t, f, "foreign", "foreign.txt")
	f.poll()
	if out := waitFile(t, f.b, "foreign"); out.State != "file_unavailable" {
		t.Fatal(out)
	}
	if n, u := fileCounts(server); n != 0 || u != 0 || f.used() != 0 {
		t.Fatal("foreign handle escaped resolver")
	}
}

func TestDeliveryFilesShareP3CapAndLeaveACompleteFileEntry(t *testing.T) {
	f := newDeliveryFixture(t)
	server := newFileFixture(t)
	f.p = server.protocol()
	fileWindow(f)
	ids := []string{"one", "two", "three", "four", "five", "six"}
	ownFiles(t, f, ids...)
	var artifacts []api.Artifact
	for _, id := range ids {
		artifacts = append(artifacts, api.Artifact{ID: id, Name: id + ".txt"})
	}
	f.snapshot.Items = []api.Item{{ID: "files", TurnKey: "turn", Kind: "tool", Status: "completed", SeenAt: f.now.UnixMicro() + 1, Artifacts: artifacts}}
	for i := 0; i < 8; i++ {
		f.poll()
		f.b.fileWait.Wait()
		f.advance(ordinaryDelay)
	}
	if n, u := fileCounts(server); n != 6 || u != 5 || f.used() != 6 || f.b.state.Window.ProgressUsed != 5 {
		t.Fatalf("P3 file cap: sends=%d uploads=%d used=%d progress=%d", n, u, f.used(), f.b.state.Window.ProgressUsed)
	}
	server.mu.Lock()
	body := server.items[0].Text.Text
	server.mu.Unlock()
	if !strings.Contains(body, "six.txt") || !strings.Contains(body, "桌面同一 Bot 对话") {
		t.Fatal("missing full file entry", body)
	}
	f.window("new")
	f.poll()
	old := f.b.state.Outputs[artifactKey("turn", "six")]
	if old.State != "deferred" || old.ArtifactID != "six" || old.Attempts != 0 || f.used() != 0 {
		t.Fatal(old, f.used())
	}
}
