package botworkspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/botskills"
)

func TestScriptSharesNativeConfigLock(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		python, err = exec.LookPath("python")
	}
	if err != nil || exec.Command(python, "-c", "import tomllib").Run() != nil {
		t.Skip("requires Python 3.11 or newer")
	}
	base := t.TempDir()
	skill, err := botskills.Install(base)
	if err != nil {
		t.Fatal(err)
	}
	notebook := filepath.Join(base, "Notebook")
	if err := os.Mkdir(notebook, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notebook, "candidate.json"), []byte(`{"command":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(notebook)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := lockConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	run := func() ([]byte, error) {
		cmd := exec.Command(python, filepath.Join(filepath.Dir(skill), "scripts", "extensions.py"), "mcp", "codex", "set", "sample", "candidate.json")
		cmd.Dir = notebook
		return cmd.CombinedOutput()
	}
	out, runErr := run()
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if runErr == nil || !strings.Contains(string(out), "busy") {
		t.Fatalf("script ignored native lock: %s (%v)", out, runErr)
	}
	if out, err := run(); err != nil {
		t.Fatalf("lock did not release: %s (%v)", out, err)
	}
	if err := WriteCodexPlugins(notebook, []byte("[mcp_servers.p_12345678_fixture]\ncommand='fixture'\n")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(notebook, codexConfig))
	if err != nil || !strings.Contains(string(body), "mcp_servers.sample") {
		t.Fatal("native projection lost script edit", err)
	}
}
