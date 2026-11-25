package chatlog

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestHistoryArchiveBackfillsOnlySentTextInDisplayOrder(t *testing.T) {
	log, db, archive := archiveFixture(t, time.FixedZone("fixture", 8*60*60))
	archive.exclude = func(item api.Item) bool { return item.RequestID == "old-command" }
	legacy := writeLegacyArchive(t, archive)
	day := time.Date(2026, 11, 23, 7, 30, 0, 0, archive.location)
	bools := true

	var old []api.Item
	for i := range pageSize + 4 {
		old = append(old, archiveItem(fmt.Sprintf("old-%03d", i), "assistant", "completed", fmt.Sprintf("old %03d", i), day.AddDate(0, 0, -5).Add(time.Duration(i)*time.Second)))
	}
	putArchiveItems(t, db, old...)
	putArchiveItems(t, db,
		archiveItem("fourth", "assistant", "completed", "at the boundary\n  indented  ", day.Add(35*time.Minute)),
		archiveItem("first", "user", "accepted", "你好\nsecond line\n\n  last line", day),
		archiveItem("second", "assistant", "completed", "answer", day.Add(15*time.Minute-time.Microsecond)),
		archiveItem("third", "user", "completed", "steer", day.Add(20*time.Minute)),
		archiveItem("same-time", "user", "accepted", "same-time", day.Add(35*time.Minute)),
		archiveItem("late", "assistant", "completed", "before midnight", day.Add(16*time.Hour+29*time.Minute)),
		archiveItem("next", "assistant", "completed", "next day", day.Add(16*time.Hour+30*time.Minute)),
		api.Item{ID: "baseline-2", Kind: "assistant", Status: "completed", Text: "undated answer"},
		api.Item{ID: "baseline-1", Kind: "user", Status: "accepted", SeenAt: -1, Text: "undated question"},
		api.Item{ID: "pending", Kind: "assistant", Status: "inProgress", SeenAt: day.UnixMicro(), Text: "excluded stream"},
		api.Item{ID: "stopped", Kind: "assistant", Status: "incomplete", SeenAt: day.UnixMicro(), Text: "excluded partial"},
		api.Item{ID: "received", Kind: "user", Status: "received", SeenAt: day.UnixMicro(), Text: "excluded ingress"},
		api.Item{ID: "sending", Kind: "user", Status: "sending", SeenAt: day.UnixMicro(), Text: "excluded sending"},
		api.Item{ID: "rejected", Kind: "user", Status: "rejected", SeenAt: day.UnixMicro(), Text: "excluded rejected"},
		api.Item{ID: "unknown", Kind: "user", Status: "unknown", SeenAt: day.UnixMicro(), Text: "excluded unknown"},
		api.Item{ID: "channel-input:remote", RequestID: "remote", Kind: "user", Status: "accepted", SeenAt: day.UnixMicro(), Text: "excluded remote command", RemoteCommand: &bools},
		api.Item{ID: "channel-input:old-command", RequestID: "old-command", Kind: "user", Status: "completed", SeenAt: day.UnixMicro(), Text: "excluded legacy command"},
		api.Item{ID: "control-input:control", RequestID: "control", Kind: "user", Status: "accepted", SeenAt: day.UnixMicro(), Text: "excluded desktop command"},
		api.Item{ID: "reply", Kind: "user", Status: "accepted", SeenAt: day.UnixMicro(), Text: "excluded receipt", QuestionReply: &api.QuestionReply{}},
		api.Item{ID: "tool", Kind: "tool", Status: "completed", SeenAt: day.UnixMicro(), Text: "excluded tool"},
		api.Item{ID: "notice", Kind: "controlNotice", Status: "completed", SeenAt: day.UnixMicro(), Text: "excluded notice"},
		api.Item{ID: "host", Kind: "hostNotice", Status: "completed", SeenAt: day.UnixMicro(), Text: "excluded host input"},
		api.Item{ID: "activation", Kind: "activation", Status: "completed", SeenAt: day.UnixMicro(), Text: "excluded activation"},
		api.Item{ID: "resident-question", Kind: "assistant", Status: "completed", SeenAt: day.UnixMicro(), Text: "excluded question card", QuestionCards: []api.QuestionCard{{Title: "synthetic"}}},
		api.Item{ID: "task-start:task", Kind: "assistant", Status: "completed", SeenAt: day.UnixMicro(), Text: "excluded task card", Task: &api.TaskPresentation{}},
		api.Item{ID: "worker-question:question", Kind: "assistant", Status: "completed", SeenAt: day.UnixMicro(), Text: "excluded worker card"},
		api.Item{ID: "skip", Kind: "assistant", Status: "completed", SeenAt: day.UnixMicro(), Text: api.SilentReminder},
		api.Item{ID: "empty", Kind: "assistant", Status: "completed", SeenAt: day.UnixMicro()},
	)
	if _, err := db.ExecContext(t.Context(), "INSERT INTO messages VALUES('malformed', 'not JSON')"); err != nil {
		t.Fatal(err)
	}

	syncArchive(t, db, archive)
	dayPath := archive.path(localArchiveDay(day))
	checkArchive(t, dayPath, "[07:30]\nuser: 你好\nsecond line\n\n  last line\n\nanswer\n\nuser: steer\n\n[08:05]\nat the boundary\n  indented  \n\nuser: same-time\n\n[23:59]\nbefore midnight\n")
	checkArchive(t, archive.path(localArchiveDay(day.AddDate(0, 0, 1))), "[00:00]\nnext day\n")
	checkArchive(t, archive.path(archiveBaseline), "undated answer\n\nuser: undated question\n")
	history := readArchive(t, archive.path(localArchiveDay(day.AddDate(0, 0, -5))))
	if !strings.HasPrefix(history, "[07:30]\nold 000\n") || !strings.HasSuffix(history, "\nold 203\n") {
		t.Fatal("existing rows outside the loaded page were lost or reordered")
	}
	if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy JSONL still present", err)
	}
	if latest, known, err := log.ConversationActivity(t.Context(), "next", archive.exclude); err != nil || known || !latest.Equal(day.Add(35*time.Minute)) {
		t.Fatalf("conversation activity changed: %v %v %v", latest, known, err)
	}
}

func TestHistoryArchiveBatchesCommittedUpdatesAndSkipsIdleWrites(t *testing.T) {
	log, db, archive := archiveFixture(t, time.Local)
	year, month, date := time.Now().Date()
	now := time.Date(year, month, date, 12, 0, 0, 0, time.Local)
	user := archiveItem("outgoing:normal", "user", "accepted", "original", now)
	user.RequestID = "normal"
	log.Observe([]api.Item{user, archiveItem("older", "assistant", "completed", "older", now.AddDate(0, 0, -10))})
	if err := log.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	ticks := make(chan time.Time)
	log.projection.Go(func() { log.runHistoryArchive(archive.root, nil, ticks) })

	localDay := localArchiveDay(now)
	path := archive.path(localDay)
	olderPath := archive.path(localArchiveDay(now.AddDate(0, 0, -10)))
	awaitArchive(t, path, "user: original")
	awaitArchive(t, olderPath, "older")
	original := archiveInfo(t, path)
	older := archiveInfo(t, olderPath)

	echo := archiveItem("native-echo", "user", "completed", "model-only wrapper", now.Add(time.Minute))
	echo.RequestID = user.RequestID
	reply := archiveItem("answer", "assistant", "inProgress", "partial", now.Add(time.Second))
	log.Observe([]api.Item{echo, reply})
	if err := log.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	tick := func() {
		t.Helper()
		select {
		case ticks <- now:
		case <-time.After(5 * time.Second):
			t.Fatal("archive loop did not accept a batch")
		}
	}
	tick()
	tick() // The preceding batch must have finished before the next receive.
	version := archiveDataVersion(t, db)
	tick()
	if got := archiveDataVersion(t, db); got != version {
		t.Fatalf("idle batches changed SQLite data version from %d to %d", version, got)
	}
	assertArchiveUnchanged(t, path, original)
	assertArchiveUnchanged(t, olderPath, older)

	reply.Text, reply.Status = "final reply", "completed"
	log.Observe([]api.Item{reply})
	if err := log.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertArchiveUnchanged(t, path, original) // A commit alone must not publish.
	tick()
	awaitArchive(t, path, "final reply")
	checkArchive(t, path, now.Format("[15:04]\n")+"user: original\n\nfinal reply\n")
	assertArchiveUnchanged(t, olderPath, older)
}

func TestHistoryArchiveRestartsAndRetriesOnlyOutstandingDates(t *testing.T) {
	log, db, archive := archiveFixture(t, time.Local)
	legacy := writeLegacyArchive(t, archive)
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local)
	day, nextDay := localArchiveDay(now), localArchiveDay(now.AddDate(0, 0, 1))
	path, next := archive.path(day), archive.path(nextDay)
	putArchiveItems(t, db, archiveItem("first", "assistant", "completed", "first", now))
	if err := os.MkdirAll(next, 0700); err != nil { // Obstruct the atomic destination.
		t.Fatal(err)
	}
	putArchiveItems(t, db, archiveItem("next", "assistant", "completed", "next", now.AddDate(0, 0, 1)))
	if err := archive.sync(t.Context(), db); err == nil {
		t.Fatal("an obstructed day was acknowledged")
	}
	checkArchive(t, path, "[12:00]\nfirst\n")
	first := archiveInfo(t, path)
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("legacy projection retired before catch-up", err)
	}
	left, err := pendingArchiveBuckets(t.Context(), db)
	if err != nil || len(left) != 1 || left[0].start != localArchiveDay(now.AddDate(0, 0, 1).UTC()) {
		t.Fatalf("wrong dates queued for retry: %v, %v", left, err)
	}
	if err := os.Remove(next); err != nil {
		t.Fatal(err)
	}
	syncArchive(t, db, archive)
	checkArchive(t, next, "[12:00]\nnext\n")
	assertArchiveUnchanged(t, path, first)
	if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy file did not retire after recovery", err)
	}

	// A complete message can commit before the next scheduled batch, even
	// while there is no archive worker running.
	putArchiveItems(t, db, archiveItem("next", "assistant", "completed", "updated after sync", now.AddDate(0, 0, 1)))
	log.Close()
	if err := removeArchiveFile(path); err != nil {
		t.Fatal(err)
	}
	reopened := Open(log.path)
	t.Cleanup(reopened.Close)
	reopened.StartHistoryArchive(archive.root, nil)
	awaitArchive(t, next, "updated after sync")
	awaitArchive(t, path, "first")
	checkArchive(t, next, "[12:00]\nupdated after sync\n")
}

func TestHistoryArchiveDoesNotAcknowledgeConcurrentSQLiteChange(t *testing.T) {
	_, db, archive := archiveFixture(t, time.UTC)
	now := time.Date(2026, 11, 24, 5, 0, 0, 0, time.UTC)
	day := localArchiveDay(now)
	item := archiveItem("one", "assistant", "completed", "before", now)
	putArchiveItems(t, db, item)
	syncArchive(t, db, archive)
	item.Text = "snapshot"
	putArchiveItems(t, db, item)
	pending, err := pendingArchiveBuckets(t.Context(), db)
	if err != nil || len(pending) != 1 {
		t.Fatal(pending, err)
	}
	content, err := archive.readDay(t.Context(), db, day)
	if err != nil {
		t.Fatal(err)
	}
	item.Text = "committed while publishing"
	item.SeenAt = now.AddDate(0, 0, 1).UnixMicro()
	putArchiveItems(t, db, item)
	if err := publishArchiveDay(archive.path(day), content); err != nil {
		t.Fatal(err)
	}
	if err := archive.acknowledge(t.Context(), db, pending, map[int64]bool{day: true}); err != nil {
		t.Fatal(err)
	}
	syncArchive(t, db, archive)
	if _, err := os.Stat(archive.path(day)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale source date remained after canonical move", err)
	}
	moved := archive.path(localArchiveDay(now.AddDate(0, 0, 1)))
	checkArchive(t, moved, "[05:00]\ncommitted while publishing\n")
	if _, err := db.ExecContext(t.Context(), "DELETE FROM messages WHERE id='one'"); err != nil {
		t.Fatal(err)
	}
	syncArchive(t, db, archive)
	if _, err := os.Stat(moved); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted SQLite content remained searchable", err)
	}
}

func TestHistoryArchiveRepartitionsAfterLocalCalendarChanges(t *testing.T) {
	_, db, original := archiveFixture(t, time.UTC)
	at := time.Date(2026, 11, 24, 20, 0, 0, 0, time.UTC)
	putArchiveItems(t, db, archiveItem("one", "assistant", "completed", "near midnight", at))
	syncArchive(t, db, original)
	oldPath := original.path(localArchiveDay(at))
	checkArchive(t, oldPath, "[20:00]\nnear midnight\n")

	loc := time.FixedZone("UTC", 8*60*60) // Even an unchanged zone name can have different rules.
	reopened := &historyArchive{root: original.root, location: loc}
	syncArchive(t, db, reopened)
	checkArchive(t, reopened.path(localArchiveDay(at.In(loc))), "[04:00]\nnear midnight\n")
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old local date remained searchable", err)
	}
}

func archiveFixture(t *testing.T, loc *time.Location) (*Log, *sql.DB, *historyArchive) {
	t.Helper()
	root := t.TempDir()
	log := Open(filepath.Join(root, "chat.sqlite"))
	t.Cleanup(log.Close)
	if err := log.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	db, err := log.open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return log, db, &historyArchive{root: filepath.Join(root, "ChatHistory"), location: loc}
}

func archiveItem(id, kind, status, text string, at time.Time) api.Item {
	return api.Item{ID: id, Kind: kind, Status: status, Text: text, SeenAt: at.UnixMicro()}
}

func putArchiveItems(t *testing.T, db *sql.DB, items ...api.Item) {
	t.Helper()
	ctx := t.Context()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, item := range items {
		body, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO messages VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", key(item), body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func writeLegacyArchive(t *testing.T, a *historyArchive) string {
	t.Helper()
	if err := os.MkdirAll(a.root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.root, "conversation.jsonl")
	if err := os.WriteFile(path, []byte("old-format-only"), 0400); err != nil {
		t.Fatal(err)
	}
	return path
}

func syncArchive(t *testing.T, db *sql.DB, a *historyArchive) {
	t.Helper()
	if err := a.sync(t.Context(), db); err != nil {
		t.Fatal(err)
	}
}

func archiveInfo(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0222 != 0 {
		t.Fatal("archive is writable")
	}
	return info
}

func archiveDataVersion(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var version int64
	if err := db.QueryRowContext(t.Context(), "PRAGMA data_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func assertArchiveUnchanged(t *testing.T, path string, before os.FileInfo) {
	t.Helper()
	after := archiveInfo(t, path)
	if !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
		t.Fatal("unchanged archive was rewritten")
	}
}

func readArchive(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func checkArchive(t *testing.T, path, want string) {
	t.Helper()
	if got := readArchive(t, path); got != want {
		t.Fatalf("archive %s = %q, want %q", filepath.Base(path), got, want)
	}
	archiveInfo(t, path)
}

func awaitArchive(t *testing.T, path, text string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		body, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(body), text) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("archive %s did not publish expected text", filepath.Base(path))
}
