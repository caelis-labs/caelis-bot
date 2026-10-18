package plugins

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// Exercise the reviewed-package launch, not just SysProcAttr field assignment.
// A console-subsystem fixture must exchange stdio without attaching a console.
func TestWindowsStdioPluginHasNoConsole(t *testing.T) {
	if os.Getenv("BOT_TEST_STDIO_CONSOLE") == "child" {
		console, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
		var input string
		if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
			os.Exit(91)
		}
		_ = json.NewEncoder(os.Stdout).Encode(struct {
			Console uintptr `json:"console"`
			Echo    string  `json:"echo"`
		}{console, input})
		os.Exit(0)
	}
	source := fixturePackage(t, false, true)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, "bin", "probe.exe"), binary, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := `{"$schema":"` + mcpSchema + `","mcpServers":{"probe":{"type":"stdio","command":"./bin/probe.exe","args":["-test.run=^TestWindowsStdioPluginHasNoConsole$"],"env":{"BOT_TEST_STDIO_CONSOLE":"child"}}}}`
	if err = os.WriteFile(filepath.Join(source, "mcp.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	if err = filepath.WalkDir(source, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(body)
		rel, relErr := filepath.Rel(source, path)
		if relErr != nil {
			return relErr
		}
		files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	entry := Entry{ID: "fixture", Title: "Fixture", Version: "0.1.0", Source: "bundled:test", Files: files, Executables: []string{"bin/probe.exe"}}
	m, err := openCatalog(t.TempDir(), []Entry{entry}, map[string]fs.FS{"fixture": os.DirFS(source)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Mutate(t.Context(), "fixture", "install", nil); err != nil {
		t.Fatal(err)
	}
	selected := m.Selection().Servers[0]
	var out, diagnostics bytes.Buffer
	if err = runStdio(t.Context(), m, "fixture", "probe", strings.NewReader("\"stdio-roundtrip\"\n"), &out, &diagnostics, filepath.Base(selected.Root)); err != nil {
		t.Fatalf("stdio plugin: %v (%s)", err, diagnostics.String())
	}
	var reply struct {
		Console uintptr `json:"console"`
		Echo    string  `json:"echo"`
	}
	if err = json.Unmarshal(out.Bytes(), &reply); err != nil {
		t.Fatalf("invalid stdio reply: %v", err)
	}
	if reply.Console != 0 || reply.Echo != "stdio-roundtrip" {
		t.Fatalf("background plugin acquired console or lost stdio: %+v", reply)
	}
}
