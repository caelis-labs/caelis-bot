package chatlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestHistoryFileIsARebuildableSQLiteProjection(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	dbPath, projection := filepath.Join(root, "chat.sqlite"), filepath.Join(root, "private", "conversation.jsonl")
	now := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	log := Open(dbPath)
	var original []api.Item
	for i := range pageSize + 4 {
		original = append(original, api.Item{ID: fmt.Sprintf("assistant-%03d", i), Kind: "assistant", Text: fmt.Sprintf("history %03d", i), Status: "completed", SeenAt: now.Add(time.Duration(i) * time.Second).UnixMicro()})
	}
	log.Observe(original)
	recorded := now.Add(24 * time.Hour)
	hidden := true
	log.Observe([]api.Item{
		{ID: "outgoing:normal", RequestID: "normal", Kind: "user", Text: "original user text", Status: "accepted", SeenAt: recorded.UnixMicro()},
		{ID: "answer", Kind: "assistant", Text: "partial", Status: "inProgress"},
		{ID: "channel-input:remote", RequestID: "remote", Kind: "user", Text: "hidden modern command", Status: "completed", SeenAt: recorded.Add(time.Hour).UnixMicro(), RemoteCommand: &hidden},
		{ID: "channel-input:old-command", RequestID: "old-command", Kind: "user", Text: "hidden legacy command", Status: "completed", SeenAt: recorded.Add(time.Hour).UnixMicro()},
		{ID: "control-input:desktop-command", RequestID: "desktop-command", Kind: "user", Text: "hidden desktop command", Status: "accepted", SeenAt: recorded.Add(time.Hour).UnixMicro()},
		{ID: "control-1", Kind: "controlNotice", Text: "hidden decision", Status: "completed"},
	})
	if err := log.flush(ctx); err != nil {
		t.Fatal(err)
	}
	log.Close()

	log = Open(dbPath)
	defer log.Close()
	exclude := func(item api.Item) bool { return item.RequestID == "old-command" }
	log.StartHistoryFile(projection, exclude)
	waitFile := func(match func(string) bool) string {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			raw, err := os.ReadFile(projection)
			if err == nil && match(string(raw)) {
				return string(raw)
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("expected committed history projection")
		return ""
	}
	body := waitFile(func(s string) bool { return strings.Contains(s, "partial") })
	if strings.Contains(body, "hidden") || !strings.Contains(body, "history 000") || strings.Index(body, "history 000") >= strings.Index(body, "history 203") {
		t.Fatal("history paging, order or visibility is wrong")
	}
	if info, err := os.Stat(projection); err != nil || info.Mode().Perm()&0222 != 0 {
		t.Fatal("projection is writable", err)
	}
	log.Observe([]api.Item{
		{ID: "native-echo", RequestID: "normal", Kind: "user", Text: "model-only wrapper", Status: "completed", SeenAt: recorded.Add(2 * time.Hour).UnixMicro()},
		{ID: "answer", Kind: "assistant", Text: "completed content", Status: "completed"},
	})
	body = waitFile(func(s string) bool { return strings.Contains(s, "completed content") })
	if strings.Contains(body, "partial") || strings.Contains(body, "model-only wrapper") || strings.Count(body, "original user text") != 1 {
		t.Fatal("same-ID update or native echo appeared twice")
	}
	if info, err := os.Stat(projection); err != nil || info.Mode().Perm()&0222 != 0 {
		t.Fatal("replacement lost the read-only attribute", err)
	}
	latest, seen, err := log.ConversationActivity(ctx, "next", exclude)
	if err != nil || seen || !latest.Equal(recorded) {
		t.Fatal(latest, seen, err)
	}
	if _, seen, err := log.ConversationActivity(ctx, "normal", exclude); err != nil || !seen {
		t.Fatal("original request identity unavailable", err)
	}

	db, err := log.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	updated := original[0]
	updated.Text = "edited canonical content"
	raw, _ := json.Marshal(updated)
	if _, err := db.ExecContext(ctx, "UPDATE messages SET body=? WHERE id=?; DELETE FROM messages WHERE id='assistant-001'", raw, updated.ID); err != nil {
		t.Fatal(err)
	}
	body = waitFile(func(s string) bool {
		return strings.Contains(s, "edited canonical content") && !strings.Contains(s, "history 001")
	})
	if strings.Contains(body, "history 000") {
		t.Fatal("superseded SQLite content remains searchable")
	}
	if err := os.Remove(projection); err != nil {
		t.Fatal(err)
	}
	if rebuilt := waitFile(func(s string) bool { return strings.Contains(s, "edited canonical content") }); rebuilt != body {
		t.Fatal("projection independently retained content")
	}
}
