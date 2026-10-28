package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAgentPluginsManifestAndMCPFailureBoundaries(t *testing.T) {
	root := fixturePackage(t, true, true)
	manifest := filepath.Join(root, "plugin.json")
	initial, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"$schema":"` + manifestSchema + `","name":"fixture","version":12}`,
		`{"$schema":"` + manifestSchema + `","name":"fixture","author":{"name":"x","extra":"y"}}`,
		`{"$schema":"` + manifestSchema + `","name":"fixture","keywords":[1]}`,
		`{"$schema":"https://agent-plugins.org/schemas/2.0.0/plugin.schema.json","name":"fixture"}`,
	} {
		if err := os.WriteFile(manifest, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(root); err == nil {
			t.Fatalf("invalid manifest accepted: %s", body)
		}
	}
	if err := os.WriteFile(manifest, []byte(`{"$schema":"`+manifestSchema+`","name":"fixture","extensions":null,"unknown":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(root)
	if err != nil || len(p.Skills) != 1 || len(p.Servers) != 2 || len(p.Issues) != 2 {
		t.Fatal("non-fatal manifest fields disabled components", err, p)
	}
	if err := os.WriteFile(manifest, initial, 0600); err != nil {
		t.Fatal(err)
	}
	mcp := filepath.Join(root, "mcp.json")
	if err := os.WriteFile(mcp, []byte(`{"$schema":"`+mcpSchema+`","mcpServers":{"good":{"type":"stdio","command":"./bin/check"},"bad":{"type":"stdio","command":"./bin/check","args":[5]},"legacy":{"type":"sse","url":"https://example.com/sse"},"odd.name":{"type":"streamable-http","url":"https://example.com/mcp"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err = Load(root)
	if err != nil || len(p.Skills) != 1 || len(p.Servers) != 2 || len(p.Issues) != 2 {
		t.Fatal("invalid server disabled siblings", err, p)
	}
	if err := os.WriteFile(mcp, []byte(`{"$schema":"`+mcpSchema+`","mcpServers":{},"unexpected":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err = Load(root)
	if err != nil || len(p.Skills) != 1 || len(p.Servers) != 0 || len(p.Issues) != 1 || p.Issues[0].Component != "mcp" {
		t.Fatal("invalid MCP top level was not isolated", err, p)
	}
}

func TestDisabledStdioGenerationStopsExistingProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	root := t.TempDir()
	source := fixturePackage(t, false, true)
	if err := os.WriteFile(filepath.Join(source, "bin/check"), []byte("#!/bin/sh\nprintf '%s\\n' \"$$\" > \"$PLUGIN_DATA/pid\"\nwhile IFS= read -r line; do printf '%s\\n' \"$line\"; done\n"), 0600); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	_ = filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		rel, _ := filepath.Rel(source, path)
		files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	entry := Entry{ID: "fixture", Title: "Fixture", Version: "0.1.0", Source: "bundled:test", Files: files, Executables: []string{"bin/check"}}
	m, err := openCatalog(root, []Entry{entry}, map[string]fs.FS{"fixture": os.DirFS(source)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Mutate(t.Context(), "fixture", "install", nil); err != nil {
		t.Fatal(err)
	}
	selected := m.Selection().Servers[0]
	launch := func(in io.Reader) error {
		// RunStdio opens its own manager in the relay process. Share the
		// committed store, not the resident manager's mutable state.
		relay, err := openCatalog(root, []Entry{entry}, map[string]fs.FS{"fixture": os.DirFS(source)})
		if err != nil {
			return err
		}
		return runStdio(t.Context(), relay, "fixture", "local-validator", in, io.Discard, io.Discard, filepath.Base(selected.Root))
	}
	in, writer := io.Pipe()
	defer writer.Close()
	done := make(chan error, 1)
	go func() {
		done <- launch(in)
	}()
	pidPath := filepath.Join(root, "data", "fixture", "pid")
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if body, err := os.ReadFile(pidPath); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(body)))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid <= 0 {
		t.Fatal("stdio child did not start")
	}
	if _, err := m.Mutate(t.Context(), "fixture", "disable", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("disabled stdio generation remained running")
	}
	if err := launch(strings.NewReader("")); err == nil || !strings.Contains(err.Error(), "no longer active") {
		t.Fatal("old selected schema could restart disabled plugin", err)
	}
}

func TestBuiltinDefaultsAndDisabledUpgrade(t *testing.T) {
	root := t.TempDir()
	source := fixturePackage(t, true, true)
	entry := func(version string) Entry {
		files := map[string]string{}
		_ = filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(body)
			rel, _ := filepath.Rel(source, path)
			files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
			return nil
		})
		return Entry{ID: "fixture", Title: "Fixture", TitleZh: "示例", Version: version, Source: "bundled:fixture", Files: files, Builtin: true, Executables: []string{"bin/check"}}
	}
	e := entry("0.1.0")
	m, err := openCatalog(root, []Entry{e}, map[string]fs.FS{"fixture": os.DirFS(source)})
	if err != nil || len(m.Selection().SkillRoots) != 1 || len(m.Selection().Servers) != 2 || m.Snapshot().Items[0].TitleZh != "示例" {
		t.Fatal("built-in was not enabled on first install", err)
	}
	oldRoot := m.Selection().Servers[0].Root
	if _, err := m.Mutate(context.Background(), "fixture", "disable", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Mutate(context.Background(), "fixture", "uninstall", nil); err == nil {
		t.Fatal("built-in package was uninstalled")
	}
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte(`{"$schema":"`+manifestSchema+`","name":"fixture","version":"0.2.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	newBinary := []byte("#!/bin/sh\n# updated executable\nexit 0\n")
	newSkill := []byte("---\nname: useful\ndescription: Updated fixture\n---\n\nUse the updated tool contract.\n")
	for path, body := range map[string][]byte{"bin/check": newBinary, "skills/useful/SKILL.md": newSkill} {
		if err := os.WriteFile(filepath.Join(source, path), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	e = entry("0.2.0")
	m, err = openCatalog(root, []Entry{e}, map[string]fs.FS{"fixture": os.DirFS(source)})
	if err != nil || m.Snapshot().Items[0].Enabled || m.Snapshot().Items[0].Version != "0.2.0" || len(m.Selection().Servers) != 0 {
		t.Fatal("disabled preference lost on built-in upgrade", err)
	}
	if _, err := m.Mutate(context.Background(), "fixture", "enable", nil); err != nil || len(m.Selection().SkillRoots) != 1 || len(m.Selection().Servers) != 2 {
		t.Fatal("enable did not restore Skill and MCP selection", err)
	}
	newRoot := m.Selection().Servers[0].Root
	if newRoot == oldRoot {
		t.Fatal("upgrade reused the old immutable package")
	}
	for path, want := range map[string][]byte{"bin/check": newBinary, "skills/useful/SKILL.md": newSkill} {
		got, err := os.ReadFile(filepath.Join(newRoot, path))
		if err != nil || string(got) != string(want) {
			t.Fatal("upgrade did not replace binary and Skill together", path, err)
		}
		old, err := os.ReadFile(filepath.Join(oldRoot, path))
		if err != nil || string(old) == string(want) {
			t.Fatal("upgrade changed the previous generation", path, err)
		}
	}
}

func TestExpansionIsSinglePassAndContained(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	s := Server{Type: "stdio", Command: "./bin/check", Args: []string{"${PLUGIN_ROOT}/x", "${UNKNOWN}"}, Env: map[string]string{"VALUE": "${PLUGIN_DATA}"}, CWD: "${PLUGIN_DATA}"}
	resolved, err := s.Resolve(root, data)
	if err != nil || resolved.CWD != data || resolved.Args[1] != "${UNKNOWN}" || resolved.Env["PLUGIN_ROOT"] != root {
		t.Fatal(resolved, err)
	}
	s.CWD = "${PLUGIN_DATA}/../escape"
	if _, err := s.Resolve(root, data); err == nil {
		t.Fatal("escaping data cwd accepted")
	}
	if !strings.HasPrefix(RuntimeName("fixture", "odd.name"), "p_") {
		t.Fatal("non-alphanumeric portable server name did not map to Runtime")
	}
}
