package localstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReplacesSamePathAndLeavesNoTemporaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unicode-用户", "state.json")
	for _, value := range []string{"first", "second"} {
		if err := Write(path, map[string]string{"value": value}); err != nil {
			t.Fatal(err)
		}
	}
	bytes, err := os.ReadFile(path)
	if err != nil || string(bytes) != "{\"value\":\"second\"}\n" {
		t.Fatalf("replacement failed: %q %v", bytes, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatal("temporary state file leaked", err, entries)
	}
	if err := SyncParent(path); err != nil {
		t.Fatal(err)
	}
}
