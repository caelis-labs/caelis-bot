package taskterminal

import (
	"context"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWindowsTemplatePreservesPathsAndExpandsOnlyLauncherArgv(t *testing.T) {
	command := `"C:\My Terminal\wt.exe" -w -1 -- {script}`
	executable, path := `C:\Bot 中文\caelis-bot.exe`, `C:\Data space\launch.command`
	args, err := WindowsTemplateArgs(command, executable, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 7 || args[0] != `C:\My Terminal\wt.exe` || args[4] != executable || args[5] != "--task-terminal-client" || args[6] != path {
		t.Fatal("Windows paths became shell fragments", args)
	}
	for _, bad := range []string{`wt.exe`, `wt.exe --command={script}`, `wt.exe {script} {script}`} {
		if ValidateCustomTemplate(bad) {
			t.Fatal("invalid template accepted", bad)
		}
	}
}

func TestWindowsHostedConsoleRejectsForeignBirth(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "launch.command")
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process)
	birth, err := processBirth(process)
	if err != nil {
		t.Fatal(err)
	}
	if err = localstate.Write(consoleIdentityPath(path, os.Getpid()), consoleIdentity{PID: os.Getpid(), Created: birth + 1}); err != nil {
		t.Fatal(err)
	}
	executable, _ := os.Executable()
	window := &hostedConsole{descriptor: path, binary: executable}
	if err = window.Confirm(t.Context(), os.Getpid()); err == nil || window.process != 0 {
		t.Fatal("PID reuse or foreign process accepted")
	}
}

func TestWindowsLiveTerminalHostClaimsExactClient(t *testing.T) {
	if os.Getenv("CAELIS_BOT_TEST_TERMINAL_HOST") != "1" && os.Getenv("CAELIS_BOT_TEST_TERMINAL_HOST") != "classic" {
		t.Skip("explicit native host acceptance")
	}
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pending, path := filepath.Join(root, "pending"), filepath.Join(root, "launch.command")
	prefix := filepath.Join(root, "accepted-")
	evidence := filepath.Join(root, "client.json")
	if err = os.WriteFile(pending, []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	launch := windowsLaunch{Version: 1, Command: LaunchArguments{Binary: exe, Directory: root, Args: []string{"--caelis-terminal-fixture", evidence, "中文 path", "literal\"quote"}, Env: map[string]string{"CODEX_HOME": root}}, Pending: pending, AcceptedPrefix: prefix}
	if err = localstate.Write(path, launch); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var window Window
	client := exe
	if c := os.Getenv("CAELIS_BOT_TEST_CONSOLE_CLIENT"); c != "" {
		client = c
	}
	command := "wt.exe -w -1 -- {script}"
	if os.Getenv("CAELIS_BOT_TEST_TERMINAL_HOST") == "classic" {
		directory, e := windows.GetSystemDirectory()
		if e != nil {
			t.Fatal(e)
		}
		command = `"` + filepath.Join(directory, "conhost.exe") + `" {script}`
	}
	window, err = launchHostedConsole(ctx, command, path, client)
	if err != nil {
		t.Fatal(err)
	}
	defer window.Release()
	var result struct {
		Args    []string
		Console bool
		Home    string
	}
	for result.Args == nil {
		data, _ := os.ReadFile(evidence)
		_ = json.Unmarshal(data, &result)
		if result.Args != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("host did not execute original connection")
		case <-time.After(50 * time.Millisecond):
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	pid := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "accepted-") {
			pid, _ = strconv.Atoi(strings.TrimPrefix(entry.Name(), "accepted-"))
		}
	}
	if pid == 0 {
		t.Fatal("missing client receipt")
	}
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	defer windows.TerminateProcess(handle, 0)
	if !result.Console || result.Home != root || len(result.Args) != 2 || result.Args[0] != "中文 path" || result.Args[1] != "literal\"quote" {
		t.Fatal("host lost real stdio or argv", result)
	}
	if err = window.Confirm(ctx, pid); err != nil {
		t.Fatal(err)
	}
	if thumbnail, err := window.(interface {
		Thumbnail(context.Context) ([]byte, error)
	}).Thumbnail(ctx); err == nil {
		if len(thumbnail) == 0 {
			t.Fatal("empty thumbnail")
		}
		t.Log("Owned console thumbnail captured")
	} else {
		t.Log("Thumbnail unavailable on this host:", err)
	}
	window.Release()
	if n, err := windows.WaitForSingleObject(handle, 0); err != nil || n != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("release killed host client")
	}
	t.Log("Terminal host executed one exact native client; stdio, Unicode argv and original process birth verified")
}
