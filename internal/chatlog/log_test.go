package chatlog

import (
	"context"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("local IM state did not recover")
}

func TestIMPersistsOnlyHumanMessagesAndReopensOffline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.sqlite")
	l := Open(path)
	l.Observe([]api.Item{{ID: "native-user", RequestID: "original-input", Kind: "user", Text: "hello", Details: "tool bytes"}, {ID: "answer", Kind: "assistant", Text: "stream"}, {ID: "tool", Kind: "activity", Text: "private tool output"}})
	l.Observe([]api.Item{{ID: "echo-user", RequestID: "original-input", Kind: "user", Text: "hello"}, {ID: "answer", Kind: "assistant", Text: "complete"}})
	before, _ := l.Snapshot()
	l.Close()
	restored := Open(path)
	defer restored.Close()
	restored.Observe([]api.Item{{ID: "echo-user", RequestID: "original-input", Kind: "user", Text: "hello"}, {ID: "answer", Kind: "assistant", Text: "complete"}})
	eventually(t, func() bool {
		restored.mu.Lock()
		defer restored.mu.Unlock()
		return restored.loaded && len(restored.dirty) == 0 && len(restored.items) == 2
	})
	items, _ := restored.Snapshot()
	if items[0].RequestID != "original-input" || items[1].Text != "complete" || items[0].Details != "" {
		t.Fatal(items)
	}
	if items[0].SeenAt != before[0].SeenAt || items[1].SeenAt != before[1].SeenAt || items[0].SeenAt <= 0 {
		t.Fatal("replayed chat items changed first-seen order", items, before)
	}
}

func TestSteeredPartialStatusSurvivesOfflineHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.sqlite")
	l := Open(path)
	partial := api.Item{ID: "same-native-item", TurnKey: "same-turn", Kind: "assistant", Text: "received partial", Status: "inProgress"}
	l.Observe([]api.Item{partial})
	partial.Status = "incomplete"
	l.Observe([]api.Item{partial, {ID: "later", TurnKey: "same-turn", Kind: "assistant", Text: "complete answer", Status: "completed"}})
	l.Close()
	restored := Open(path)
	defer restored.Close()
	eventually(t, func() bool { items, _ := restored.Snapshot(); return len(items) == 2 })
	items, _ := restored.Snapshot()
	if items[0].ID != partial.ID || items[0].Text != partial.Text || items[0].Status != "incomplete" || items[1].Status != "completed" {
		t.Fatal("offline transcript lost item identity, bytes or finality", items)
	}
}

func TestDiskFailureRetainsLiveIMAndAutomaticallyRetries(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "blocked")
	os.WriteFile(parent, []byte("obstruction"), 0600)
	path := filepath.Join(parent, "chat.sqlite")
	l := Open(path)
	l.Observe([]api.Item{{ID: "user", Kind: "user", Text: "still usable"}})
	items, _ := l.Snapshot()
	if len(items) != 1 {
		t.Fatal("storage error erased live message")
	}
	os.Remove(parent)
	os.Mkdir(parent, 0700)
	eventually(t, func() bool { l.mu.Lock(); defer l.mu.Unlock(); return l.loaded && len(l.dirty) == 0 })
	l.Close()
	l = Open(path)
	defer l.Close()
	eventually(t, func() bool { items, _ := l.Snapshot(); return len(items) == 1 })
}

func TestLocalEarlierMessagesAreAvailableWithoutRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.sqlite")
	l := Open(path)
	var items []api.Item
	for n := 0; n < 450; n++ {
		items = append(items, api.Item{ID: fmt.Sprint(n), Kind: "user", Text: fmt.Sprint(n)})
	}
	l.Observe(items)
	l.Close()
	l = Open(path)
	defer l.Close()
	eventually(t, func() bool { items, earlier := l.Snapshot(); return len(items) == 200 && earlier })
	if err := l.LoadEarlier(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := l.LoadEarlier(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, earlier := l.Snapshot()
	if len(got) != 450 || earlier || got[0].Text != "0" || got[449].Text != "449" {
		t.Fatal(len(got), earlier)
	}
}

func TestCorruptDisplayCacheRebuildsAndPreservesDamagedBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.sqlite")
	if err := os.WriteFile(path, []byte("damaged IM database"), 0600); err != nil {
		t.Fatal(err)
	}
	l := Open(path)
	l.Observe([]api.Item{{ID: "original-answer", Kind: "assistant", Text: "still online"}})
	defer l.Close()
	eventually(t, func() bool { l.mu.Lock(); defer l.mu.Unlock(); return l.loaded && len(l.dirty) == 0 && l.issue == nil })
	files, _ := filepath.Glob(path + ".damaged-*")
	if len(files) != 1 {
		t.Fatal("corrupt bytes not preserved", files)
	}
	if got, _ := os.ReadFile(files[0]); string(got) != "damaged IM database" {
		t.Fatal("changed damaged display cache")
	}
	items, _ := l.Snapshot()
	if len(items) != 1 || items[0].ID != "original-answer" {
		t.Fatal("lost live display identity")
	}
}
