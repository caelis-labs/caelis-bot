package taskterminal

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in: exercise the production launcher and receipt using a
// synthetic CLI. No runtime/worker command or user account is involved.
func TestInstalledTerminalAdapters(t *testing.T) {
	configured := os.Getenv("CAELIS_BOT_TEST_TERMINALS")
	if configured == "" {
		t.Skip("set CAELIS_BOT_TEST_TERMINALS to comma-separated installed terminal IDs")
	}
	for _, id := range strings.Split(configured, ",") {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "receipt")
			binary := filepath.Join(dir, "synthetic-runtime")
			script := "#!/bin/sh\nprintf '\\033]0;Caelis Bot terminal verification\\007'\nprintf '%s\\n' 'Caelis Bot: terminal adapter and confirmation verified.' 'Press Enter to close this test.'\nprintf '%s' verified > " + quote(marker) + "\nsleep 1\nread answer\n"
			if e := os.WriteFile(binary, []byte(script), 0700); e != nil {
				t.Fatal(e)
			}
			launcher := New(filepath.Join(dir, "launch"), func(ctx context.Context, path string) error {
				args, e := OpenArgs(id, path)
				if e != nil {
					return e
				}
				return exec.CommandContext(ctx, "/usr/bin/open", args...).Run()
			})
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			defer cancel()
			target := api.TerminalTarget{Runtime: "codex", Binary: binary, Directory: dir, Endpoint: "unix:///tmp/fixture.sock", Thread: "synthetic-owned"}
			if e := launcher.Open(ctx, "synthetic", target); e != nil {
				t.Fatal(e)
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if b, e := os.ReadFile(marker); e == nil && string(b) == "verified" {
					t.Log("production launch waited for consent and executed the synthetic runtime")
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal("script was confirmed but the synthetic runtime did not execute")
		})
	}
}
