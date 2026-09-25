package taskterminal

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCustomTerminalParsesArgvAndDoesNotInterpolateScript(t *testing.T) {
	script := "/private/folder with ' quotes/$(unexpected).command"
	args, err := CustomArgs(`"/Applications/My Terminal.app/Contents/MacOS/term" start --title 'Bot work' -- /bin/sh {script}`, script)
	want := []string{"/Applications/My Terminal.app/Contents/MacOS/term", "start", "--title", "Bot work", "--", "/bin/sh", script}
	if err != nil || !reflect.DeepEqual(args, want) {
		t.Fatal(args, err)
	}
	for _, bad := range []string{"", `term`, `term {script} {script}`, `term --file={script}`, `{script} arg`, `term "unfinished {script}`, `term \`, "term\x00 {script}"} {
		if _, err := CustomArgs(bad, script); err == nil {
			t.Fatal("bad template accepted", bad)
		}
	}
	if PreferenceForBundle("COM.GOOGLECODE.ITERM2") != "iterm2" || PreferenceForBundle("unknown") != "" {
		t.Fatal("unverified bundle mapping")
	}
}

func TestCustomTerminalUsesTheSameExecutionReceipt(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-runtime")
	marker := filepath.Join(dir, "ran")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf ready > "+quote(marker)+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	l := New(filepath.Join(dir, "launch"), func(ctx context.Context, path string) error { return LaunchCustom(ctx, "/bin/sh {script}", path) })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := l.Open(ctx, "owned", api.TerminalTarget{Runtime: "codex", Binary: binary, Directory: dir, Endpoint: "unix:///tmp/local.sock", Thread: "owned"}); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("runtime did not start")
		case <-time.After(time.Millisecond):
		}
	}
	if err := LaunchCustom(t.Context(), "/missing-terminal {script}", "/tmp/probe"); err == nil {
		t.Fatal("missing executable accepted")
	}
}
