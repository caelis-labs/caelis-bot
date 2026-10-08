package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestToolIndexConnectedDirectoryOnlyAndNoopWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app-skills", "mcp-tools.json")
	servers := []IndexServer{{PackageID: "other", Name: "search", RuntimeName: "p_other_search", Tools: []Tool{{Name: "find_other"}}}, {PackageID: "notes", Name: "search", RuntimeName: "p_fixture_search", Tools: []Tool{
		{Name: "lookup", Description: "Find matching notes"},
		{Name: "unsafe", Description: "token: private value"},
	}}}
	if err := WriteIndex(path, servers); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), `"name":"lookup"`) || !strings.Contains(string(first), `"description":"Find matching notes"`) || !strings.Contains(string(first), `"package":"other","service":"search","runtime":"p_other_search"`) || strings.Contains(string(first), "private value") || strings.Contains(string(first), "schema") {
		t.Fatalf("index leaked or omitted directory metadata: %s", first)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stamp := info.ModTime()
	time.Sleep(10 * time.Millisecond)
	if err := WriteIndex(path, servers); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(path)
	if err != nil || !info.ModTime().Equal(stamp) {
		t.Fatal("unchanged index was rewritten", err)
	}
	if err := WriteIndex(path, nil); err != nil {
		t.Fatal(err)
	}
	cleared, err := os.ReadFile(path)
	if err != nil || string(cleared) != "{\"services\":[]}\n" {
		t.Fatalf("disconnected service remained: %s %v", cleared, err)
	}
}
