package taskterminal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestMissingTaskWorkspaceNeverLaunches(t *testing.T) {
	for _, runtime := range []string{"codex", "caelis"} {
		for _, replacedByFile := range []bool{false, true} {
			t.Run(runtime+map[bool]string{false: "/deleted", true: "/file"}[replacedByFile], func(t *testing.T) {
				root := t.TempDir()
				workspace := filepath.Join(root, "工作 space")
				if err := os.Mkdir(workspace, 0700); err != nil {
					t.Fatal(err)
				}
				target := api.TerminalTarget{Runtime: runtime, Binary: filepath.Join(root, "runtime.exe"), Directory: workspace, Endpoint: "http://127.0.0.1:1234", Thread: "original-thread", Session: "original-session", Store: root, TokenFile: filepath.Join(root, "token")}
				if runtime == "codex" {
					target.Endpoint = "unix://" + filepath.Join(root, "runtime.sock")
				}
				if err := os.Remove(workspace); err != nil {
					t.Fatal(err)
				}
				if replacedByFile {
					if err := os.WriteFile(workspace, nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				launcher := New(filepath.Join(root, "Terminal"), func(context.Context, string) error {
					t.Fatal("unavailable workspace launched a terminal")
					return nil
				})
				err := launcher.Open(t.Context(), "original-task", target)
				if !errors.Is(err, api.ErrTaskWorkspaceUnavailable) || !errors.Is(err, ErrLaunchNotSubmitted) {
					t.Fatal("missing safe pre-launch rejection", err)
				}
				if _, err := os.Stat(launcher.directory); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("published an unnecessary launch attempt", err)
				}
				if !replacedByFile {
					if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("recreated workspace", err)
					}
				}
			})
		}
	}
}
