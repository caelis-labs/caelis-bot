package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

func TestReviewedMarketplaceTwoPortablePackages(t *testing.T) {
	files := fstest.MapFS{
		"plugins/writer/plugin.json":           {Data: []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"writer","version":"1.2.0","description":"Draft documents","author":{"name":"Aster"}}`)},
		"plugins/writer/skills/draft/SKILL.md": {Data: []byte("---\nname: draft\ndescription: Draft documents.\n---\n\nWrite a clear draft.\n")},
		"plugins/writer/assets/sample.txt":     {Data: []byte("Reviewed supporting resource\n")},
		"plugins/search/plugin.json":           {Data: []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"search","version":"2.0.0","description":"Search a service","author":{"name":"Northwind"}}`)},
		"plugins/search/mcp.json":              {Data: []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"search":{"type":"stdio","command":"./bin/server"}}}`)},
		"plugins/search/bin/server":            {Data: []byte("#!/bin/sh\nexit 0\n"), Mode: 0755},
	}
	approved := map[string]map[string]string{"writer": {}, "search": {}}
	for name, file := range files {
		sum := sha256.Sum256(file.Data)
		if len(name) > len("plugins/writer/") && name[:len("plugins/writer/")] == "plugins/writer/" {
			approved["writer"][name[len("plugins/writer/"):]] = hex.EncodeToString(sum[:])
		} else {
			approved["search"][name[len("plugins/search/"):]] = hex.EncodeToString(sum[:])
		}
	}
	index := []byte(`{"name":"community","plugins":[{"name":"writer","source":{"source":"local","path":"./plugins/writer"},"interface":{"displayName":"Writer"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"}},{"name":"search","source":{"source":"local","path":"./plugins/search"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"}}]}`)
	m, err := OpenReviewedMarketplace(t.TempDir(), files, index, approved)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := m.Snapshot()
	if len(snapshot.Items) != 2 || len(snapshot.Items[0].Skills) != 1 || len(snapshot.Items[1].MCPServers) != 1 || snapshot.Items[1].MCPServers[0].Description != "Search a service" || snapshot.Items[0].Publisher != "Aster" || snapshot.Items[1].Publisher != "Northwind" {
		t.Fatalf("generic catalog metadata mismatch: %+v", snapshot.Items)
	}
	if _, err := m.Mutate(context.Background(), "writer", "install", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Mutate(context.Background(), "search", "install", nil); err != nil {
		t.Fatal(err)
	}
	if len(m.Selection().SkillRoots) != 1 || len(m.Selection().Servers) != 1 {
		t.Fatalf("portable contributions not selected: %+v", m.Selection())
	}
	serverRoot := m.Selection().Servers[0].Root
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(filepath.Join(serverRoot, "bin", "server")); err != nil || info.Mode().Perm()&0111 == 0 {
			t.Fatalf("reviewed CLI lost its executable mode: %v %v", info, err)
		}
	}
	if body, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(m.Selection().SkillRoots[0])), "assets", "sample.txt")); err != nil || string(body) != "Reviewed supporting resource\n" {
		t.Fatalf("reviewed resource missing: %q %v", body, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(serverRoot, "bin", "server"), 0600); err != nil {
			t.Fatal(err)
		}
		if m.Snapshot().Items[1].Status != "failed" {
			t.Fatal("executable mode tamper not detected")
		}
		if _, err := m.Mutate(context.Background(), "search", "uninstall", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Mutate(context.Background(), "search", "install", nil); err != nil {
			t.Fatal("reviewed reinstall did not recover", err)
		}
		if m.Selection().Servers[0].Root == serverRoot {
			t.Fatal("corrupt old root reused")
		}
	}
	if detail, err := m.SkillDetail("writer", "draft"); err != nil || detail.Body != "Write a clear draft." {
		t.Fatalf("skill detail: %+v %v", detail, err)
	}
	approved["writer"]["plugin.json"] = "bad"
	if _, err := OpenReviewedMarketplace(t.TempDir(), files, index, approved); err == nil {
		t.Fatal("tampered catalog package accepted")
	}
	if _, _, err := NormalizeMarketplace(files, []byte(`{"name":"community","plugins":[{"name":"writer","source":{"source":"local","path":"./../escape"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"}}]}`), approved); err == nil {
		t.Fatal("escaping marketplace path accepted")
	}
}

func TestUnavailableReviewedSourceFailsWithoutPanic(t *testing.T) {
	e := Entry{ID: "missing", Title: "Missing", Version: "1.0.0", Description: "Missing", Source: "bundled:test", Files: map[string]string{"plugin.json": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	m, err := openCatalog(t.TempDir(), []Entry{e}, map[string]fs.FS{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.SkillDetail("missing", "sample"); err == nil {
		t.Fatal("missing source returned a skill")
	}
	if _, err = m.Mutate(context.Background(), "missing", "install", nil); err == nil {
		t.Fatal("missing source was staged")
	}
}

func TestBundledSkillDetailPreservesLongGuidance(t *testing.T) {
	m, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	detail, err := m.SkillDetail("markdown-work", "markdown-work")
	if err != nil {
		t.Fatal(err)
	}
	for _, sentence := range []string{"Start with the user's purpose", "Preserve source facts", "Before handing over"} {
		if !strings.Contains(detail.Body, sentence) {
			t.Fatalf("long reviewed paragraph was omitted: %q", sentence)
		}
	}
}

func TestMarketplaceRejectsSymlinkAtPackageRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require developer mode")
	}
	root := t.TempDir()
	outside := t.TempDir()
	manifest := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"writer","version":"1.0.0","description":"Write"}`)
	if err := os.WriteFile(filepath.Join(outside, "plugin.json"), manifest, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "plugins"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "plugins", "writer")); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(manifest)
	approved := map[string]map[string]string{"writer": {"plugin.json": hex.EncodeToString(sum[:])}}
	index := []byte(`{"name":"community","plugins":[{"name":"writer","source":{"source":"local","path":"./plugins/writer"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"}}]}`)
	if _, _, err := NormalizeMarketplace(os.DirFS(root), index, approved); err == nil {
		t.Fatal("symlinked package root escaped marketplace filesystem")
	}
}
