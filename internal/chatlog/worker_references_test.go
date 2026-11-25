package chatlog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestWorkerReferencesReadAllHistoryAndDirtyIdentitiesWithoutChatText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.sqlite")
	seedWorkerReferenceHistory(t, path)
	l := Open(path)
	defer l.Close()
	waitForWorkerReferenceHistory(t, l)
	if visible, earlier := l.Snapshot(); len(visible) != pageSize || !earlier {
		t.Fatal("fixture did not exercise paginated history", len(visible), earlier)
	}
	l.Observe([]api.Item{{ID: "dirty-original", RequestID: "unflushed-original", Kind: "user", Text: "PRIVATE_DIRTY_SENTINEL", Artifacts: []api.Artifact{{ID: "upload-file:opaque-fixture"}}}})
	refs, err := l.WorkerFileReferences(t.Context())
	oldest := api.TaskFileReference{TaskID: "original-task", ID: "ref-0"}
	unflushed := api.TaskFileReference{ID: "upload-file:opaque-fixture"}
	if err != nil || !refs.Complete || len(refs.Submissions) != 321 || len(refs.References) != 321 ||
		!slices.Contains(refs.Submissions, "original-submission-0") || !slices.Contains(refs.Submissions, "unflushed-original") ||
		!slices.Contains(refs.References, oldest) || !slices.Contains(refs.References, unflushed) {
		t.Fatal("visible window or flush race lost original file identities", len(refs.Submissions), len(refs.References), refs.Complete, err)
	}
	encoded, _ := json.Marshal(refs)
	if strings.Contains(string(encoded), "PRIVATE_") {
		t.Fatal("maintenance returned chat or artifact display text")
	}
	// Reopen the existing log instead of relying on the previous visible cache.
	l.Close()
	reopened := Open(path)
	defer reopened.Close()
	waitForWorkerReferenceHistory(t, reopened)
	refs, err = reopened.WorkerFileReferences(t.Context())
	if err != nil || !refs.Complete || !slices.Contains(refs.Submissions, "original-submission-0") || !slices.Contains(refs.References, oldest) {
		t.Fatal("restart lost older protected identities", err)
	}
}

func seedWorkerReferenceHistory(t *testing.T, path string) {
	t.Helper()
	seed := &Log{path: path}
	db, err := seed.open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 320; i++ {
		id := "original-submission-" + strconv.Itoa(i)
		item := api.Item{ID: "input:" + id, Kind: "user", RequestID: id, Text: "PRIVATE_CHAT_SENTINEL", Artifacts: []api.Artifact{{ID: api.TaskArtifactID("original-task", "ref-"+strconv.Itoa(i)), Name: "PRIVATE_NAME_SENTINEL"}}}
		body, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), `INSERT INTO messages(id,body) VALUES(?,?)`, item.ID, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func waitForWorkerReferenceHistory(t *testing.T, l *Log) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		l.mu.Lock()
		loaded := l.loaded
		l.mu.Unlock()
		if loaded {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture chat did not load", l.Status())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestWorkerReferencesHonorCancellationDuringFlush(t *testing.T) {
	l := &Log{path: filepath.Join(t.TempDir(), "chat.sqlite")}
	if err := l.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	db, err := l.open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Hold the lock order used by flush and force SQLite into the exclusive
	// phase a write can reach at commit, independent of filesystem timing.
	l.flushMu.Lock()
	defer l.flushMu.Unlock()
	if _, err := db.ExecContext(t.Context(), `BEGIN EXCLUSIVE`); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`ROLLBACK`)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	refs, err := l.WorkerFileReferences(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || refs.Complete {
		t.Fatalf("reference scan during flush: complete=%v, err=%v", refs.Complete, err)
	}
}

func TestWorkerReferencesRemainIncompleteBeforeLoadAndAfterDamage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-created.sqlite")
	l := &Log{path: path, dirty: map[string]api.Item{"pending": {RequestID: "unknown-original", Text: "PRIVATE_DIRTY_SENTINEL"}}}
	refs, err := l.WorkerFileReferences(t.Context())
	if err != nil || refs.Complete || !slices.Contains(refs.Submissions, "unknown-original") {
		t.Fatal("unloaded history was treated as a complete unreferenced set", refs, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("reference inventory created a chat database", err)
	}
	db, err := l.open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO messages(id,body) VALUES('corrupt', 'invalid-json')`)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	l.loaded = true
	refs, err = l.WorkerFileReferences(t.Context())
	if err == nil || refs.Complete || !slices.Contains(refs.Submissions, "unknown-original") {
		t.Fatal("damaged history authorized attachment retirement", refs, err)
	}
}

func TestWorkerReferenceReadOnlyDatabaseURI(t *testing.T) {
	for _, c := range []struct{ path, want string }{
		{`C:\Users\Synthetic User\chat.sqlite`, "file:///C:/Users/Synthetic%20User/chat.sqlite?mode=ro"},
		{"D:/isolated/中文/chat.sqlite", "file:///D:/isolated/%E4%B8%AD%E6%96%87/chat.sqlite?mode=ro"},
		{"/isolated/name?#%.sqlite", "file:///isolated/name%3F%23%25.sqlite?mode=ro"},
	} {
		if got := sqliteDatabaseURI(c.path, "ro"); got != c.want {
			t.Errorf("read-only URI %q: got %q want %q", c.path, got, c.want)
		}
	}
}
