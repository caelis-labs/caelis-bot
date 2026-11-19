package botworkspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexPluginProjectionPreservesNativeSettings(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, codexConfig)
	original := "# personal settings\n[features]\nweb_search = false\n[mcp_servers]\n[mcp_servers.existing]\nurl = 'https://example.test/old'\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	generated := "[mcp_servers.p_01234567_plugin]\ncommand = 'fixture'\n"
	if err := WriteCodexPlugins(workspace, []byte(generated)); err != nil {
		t.Fatal(err)
	}
	prior, err := os.ReadFile(path)
	if want := original + "# BEGIN Caelis Bot plugins\n" + generated + "# END Caelis Bot plugins\n"; err != nil || string(prior) != want {
		t.Fatalf("projected configuration: %v\n%s", err, prior)
	}
	standalone := "# BEGIN Caelis Bot workspace MCP\n[mcp_servers.fresh]\nurl = 'https://example.test/new'\n# END Caelis Bot workspace MCP\n"
	if err := os.WriteFile(path, append(prior, standalone...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteCodexPlugins(workspace, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != original+standalone {
		t.Fatalf("configuration lost its ownership boundaries: %v\n%s", err, body)
	}
}

func TestCodexPluginProjectionIgnoresMarkersInMultilineValues(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, codexConfig)
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	const base = "basic = \"\"\"\n" + pluginBlockStart + "\nquote: \\\"\"\"\n" + pluginBlockEnd + "\n\"\"\"\"\n" +
		"literal = '''\n" + pluginBlockEnd + "\n" + pluginBlockStart + "\n'''''\n" +
		"\"\" = '\"' # '''\nvalues = [nan, -0.0]\n[[profiles]]\nspeed = nan\n"
	const old = "[mcp_servers.p_01234567_plugin]\nargs = ['old']\n"
	const next = "[mcp_servers.p_01234567_plugin]\nargs = ['new']\n"
	if err := os.WriteFile(path, appendPluginBlock([]byte(base), []byte(old)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, generated := range []string{next, ""} {
		if err := WriteCodexPlugins(workspace, []byte(generated)); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(path)
		if want := appendPluginBlock([]byte(base), []byte(generated)); err != nil || string(body) != string(want) {
			t.Fatalf("multiline config changed: %v\n%s", err, body)
		}
	}
}

func TestCodexPluginProjectionRejectsConfigConflicts(t *testing.T) {
	const plugin = "[mcp_servers.p_01234567_plugin]\ncommand = 'fixture'\n"
	const block = pluginBlockStart + "\n" + plugin + pluginBlockEnd + "\n"
	for _, test := range []struct {
		name, original, generated string
	}{
		{"duplicate marker", block + pluginBlockStart + "\n", ""},
		{"unmatched marker", pluginBlockEnd + "\n", ""},
		{"unterminated block", pluginBlockStart + "\n" + plugin, ""},
		{"invalid original", block + "malformed = [\n", ""},
		{"unrelated setting in block", pluginBlockStart + "\n[features]\nweb_search = false\n" + pluginBlockEnd + "\n", ""},
		{"unrelated trailing assignment", "[features]\nweb_search = false\n" + block + "enabled = false\n", ""},
		{"server id already configured", "[mcp_servers.p_01234567_plugin.env]\nNATIVE = 'value'\n", plugin},
		{"unrelated generated setting", "", "[features]\nweb_search = false\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			path := filepath.Join(workspace, codexConfig)
			if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.original), 0600); err != nil {
				t.Fatal(err)
			}
			if err := WriteCodexPlugins(workspace, []byte(test.generated)); err == nil {
				t.Fatal("expected a project configuration conflict")
			}
			body, err := os.ReadFile(path)
			if err != nil || string(body) != test.original {
				t.Fatalf("configuration changed on conflict: %v\n%s", err, body)
			}
		})
	}
}

func TestCodexWriteRejectsStaleProjectFile(t *testing.T) {
	workspace := t.TempDir()
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir(".codex", 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, codexConfig)
	if err := os.WriteFile(path, []byte("# before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	prior, err := readRegular(root, codexConfig)
	if err != nil {
		t.Fatal(err)
	}
	const edited = "# changed by the workspace\n"
	if err := os.WriteFile(path, []byte(edited), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeCodex(root, prior, []byte("# app projection\n")); !errors.Is(err, ErrProjectWriteBusy) {
		t.Fatal("expected a project write conflict", err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != edited {
		t.Fatalf("project conflict lost a native edit: %v\n%s", err, body)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("project conflict left staging files", entries, err)
	}
}

func TestCodexRollbackRestoresNativeConfiguration(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, codexConfig)
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	const native = "[mcp_servers.personal]\ncommand = 'personal'\n"
	if err := os.WriteFile(path, []byte(native), 0600); err != nil {
		t.Fatal(err)
	}
	project := func(server string) {
		t.Helper()
		if err := WriteCodexPlugins(workspace, []byte(legacyPluginMarker+server)); err != nil {
			t.Fatal(err)
		}
	}
	project("[mcp_servers.p_01234567_removed]\ncommand = 'old'\n")
	// Old Bot checks the leading marker and replaces the entire project.
	prior, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(prior), legacyPluginMarker) {
		t.Fatal("legacy Bot cannot open project", err)
	}
	if err := os.WriteFile(path, []byte(legacyPluginMarker), 0600); err != nil {
		t.Fatal(err)
	}
	project("")
	project("")
	body, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(body), native) || strings.Contains(string(body), "removed") {
		t.Fatal("upgrade lost native settings or restored a removed plugin", err)
	}
}
