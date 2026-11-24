package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

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
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	// The installer creates the only on-disk copy. Source bytes stay in
	// memory while the parent test keeps its original executable mapped.
	manifest := `{"$schema":"` + mcpSchema + `","mcpServers":{"probe":{"type":"stdio","command":"./bin/probe.exe","args":["-test.run=^TestWindowsStdioPluginHasNoConsole$"],"env":{"BOT_TEST_STDIO_CONSOLE":"child"}}}}`
	source := fstest.MapFS{
		"plugin.json":   {Data: []byte(`{"$schema":"` + manifestSchema + `","name":"fixture","version":"0.1.0"}`)},
		"mcp.json":      {Data: []byte(manifest)},
		"bin/probe.exe": {Data: image},
	}
	files := make(map[string]string, len(source))
	for name, file := range source {
		sum := sha256.Sum256(file.Data)
		files[name] = hex.EncodeToString(sum[:])
	}

	entry := Entry{ID: "fixture", Title: "Fixture", Version: "0.1.0", Source: "bundled:test", Files: files, Executables: []string{"bin/probe.exe"}}
	m, err := openCatalog(t.TempDir(), []Entry{entry}, map[string]fs.FS{"fixture": source})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Mutate(t.Context(), "fixture", "install", nil); err != nil {
		t.Fatal(err)
	}
	selected := m.Selection().Servers[0]
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var out, diagnostics bytes.Buffer
	if err = runStdio(ctx, m, "fixture", "probe", strings.NewReader("\"stdio-roundtrip\"\n"), &out, &diagnostics, filepath.Base(selected.Root)); err != nil {
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
