package localstate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAtomicWriteSurvivesBriefReceiptReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	if err := WriteConfirmed(path, map[string]string{"state": "waiting"}); err != nil {
		t.Fatal(err)
	}
	// os.Open's Windows handle does not share DELETE access. A UI polling the
	// receipt can therefore overlap its atomic replacement, without modifying it.
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	done := make(chan error, 1)
	go func() { done <- WriteConfirmed(path, map[string]string{"state": "failed"}) }()
	select {
	case err := <-done:
		t.Fatalf("write ended while reader still held the destination: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("receipt replacement did not recover after reader closed")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "{\"state\":\"failed\"}\n" {
		t.Fatalf("receipt was not atomically replaced: %s %v", body, err)
	}
}
