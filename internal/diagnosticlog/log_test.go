package diagnosticlog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRotationBoundsConcurrentWritesAndRetainsValidRecords(t *testing.T) {
	l := New(filepath.Join(t.TempDir(), "Logs"))
	l.maxBytes, l.maxFiles = 1024, 3
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				l.Write(Record{Component: "codex", Code: "component_failed", Method: "mcpServer/startupStatus/updated"})
			}
		})
	}
	wg.Wait()
	files, err := os.ReadDir(l.dir)
	if err != nil || len(files) != l.maxFiles {
		t.Fatalf("rotation: %d files, %v", len(files), err)
	}
	for _, file := range files {
		info, _ := file.Info()
		if info.Size() > l.maxBytes || info.Mode().Perm() != 0600 {
			t.Fatal("unbounded or public log")
		}
		f, _ := os.Open(filepath.Join(l.dir, file.Name()))
		scan := bufio.NewScanner(f)
		for scan.Scan() {
			var r Record
			if json.Unmarshal(scan.Bytes(), &r) != nil || r.Time.IsZero() || r.Code != "component_failed" {
				t.Fatal("interleaved/truncated record")
			}
		}
		if err := scan.Err(); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	if l.written != 160 || l.failed != 0 {
		t.Fatal(l.Status())
	}
	info, _ := os.Stat(l.dir)
	if info.Mode().Perm() != 0700 {
		t.Fatal("public diagnostic directory")
	}
}

func TestRetentionAfterRestartAndWriteFailureDoesNotDestroyFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Logs")
	l := New(dir)
	l.Write(Record{Code: "old"})
	old := time.Now().Add(-MaxAge - time.Hour)
	if err := os.Chtimes(l.path(0), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.path(1), []byte("old private log"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(l.path(1), old, old); err != nil {
		t.Fatal(err)
	}
	l = New(dir)
	l.Write(Record{Code: "new"})
	b, _ := os.ReadFile(l.path(0))
	if strings.Contains(string(b), `"old"`) {
		t.Fatal("expired active log survived restart")
	}
	if _, err := os.Stat(l.path(1)); !os.IsNotExist(err) {
		t.Fatal("expired archive retained")
	}
	if err := os.Remove(l.path(0)); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(t.TempDir(), "sentinel")
	os.WriteFile(private, []byte("keep"), 0600)
	if err := os.Symlink(private, l.path(0)); err != nil {
		t.Skip("symlink unavailable")
	}
	l.Write(Record{Code: "must_not_follow"})
	b, _ = os.ReadFile(private)
	if string(b) != "keep" || l.failed != 1 {
		t.Fatal("followed symlink or concealed write failure")
	}
}

func TestNativeErrorDetailsAreClassifiedNotCopied(t *testing.T) {
	const secret = "AUTH_AND_CONVERSATION_SENTINEL"
	l := New(filepath.Join(t.TempDir(), "Logs"))
	message := "No such file or directory; Authorization: Bearer " + secret
	l.Write(Record{Code: "component_failed", Server: "bad\n" + secret, Reason: Reason(message), Fingerprint: Fingerprint([]byte(message))})
	b, err := os.ReadFile(l.path(0))
	if err != nil || strings.Contains(string(b), secret) || !strings.Contains(string(b), "executable or file not found") || !strings.Contains(string(b), Fingerprint([]byte(message))) {
		t.Fatal("private error leaked or correlation missing")
	}
}
