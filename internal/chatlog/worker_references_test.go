package chatlog

import (
	"encoding/json"
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
	seed := &Log{path: path}
	db, err := seed.open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 320; i++ {
		id := "original-submission-" + strconv.Itoa(i)
		item := api.Item{ID: "input:" + id, Kind: "user", RequestID: id, Text: "PRIVATE_CHAT_SENTINEL", Artifacts: []api.Artifact{{ID: api.TaskArtifactID("original-task", "ref-"+strconv.Itoa(i)), Name: "PRIVATE_NAME_SENTINEL"}}}
		body, _ := json.Marshal(item)
		if _, err := db.Exec(`INSERT INTO messages(id,body) VALUES(?,?)`, item.ID, body); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()
	l := Open(path)
	defer l.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		l.mu.Lock()
		loaded := l.loaded
		l.mu.Unlock()
		if loaded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture chat did not load")
		}
		time.Sleep(time.Millisecond)
	}
	if visible, earlier := l.Snapshot(); len(visible) > 200 || !earlier {
		t.Fatal("fixture did not exercise paginated history", len(visible), earlier)
	}
	l.Observe([]api.Item{{ID: "dirty-original", RequestID: "unflushed-original", Kind: "user", Text: "PRIVATE_DIRTY_SENTINEL", Artifacts: []api.Artifact{{ID: "upload-file:opaque-fixture"}}}})
	refs, err := l.WorkerFileReferences(t.Context())
	if err != nil || !refs.Complete || len(refs.Submissions) != 321 || len(refs.References) != 321 || !slices.Contains(refs.Submissions, "original-submission-0") || !slices.Contains(refs.Submissions, "unflushed-original") {
		t.Fatal("visible window or flush race lost original file identities", len(refs.Submissions), len(refs.References), refs.Complete, err)
	}
	encoded, _ := json.Marshal(refs)
	if strings.Contains(string(encoded), "PRIVATE_") {
		t.Fatal("maintenance returned chat or artifact display text")
	}
	// Reopen the existing log rather than relying on a previous visible cache.
	l.Close()
	l = Open(path)
	defer l.Close()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		refs, err = l.WorkerFileReferences(t.Context())
		if err == nil && refs.Complete {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil || !refs.Complete || !slices.Contains(refs.Submissions, "original-submission-0") {
		t.Fatal("restart lost older protected identities", err)
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
