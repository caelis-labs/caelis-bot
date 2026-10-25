package taskterminal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"golang.org/x/sys/windows"
)

func init() {
	if handled, err := RunClient(os.Args[1:]); handled {
		if err != nil {
			os.Exit(9)
		}
		os.Exit(0)
	}
	if len(os.Args) < 3 || os.Args[1] != "--caelis-terminal-fixture" {
		return
	}
	if hwnd, _, _ := terminalKernel.NewProc("GetConsoleWindow").Call(); hwnd == 0 {
		terminalKernel.NewProc("AttachConsole").Call(uintptr(uint32(0xffffffff)))
	}
	console := true
	for _, std := range []uint32{windows.STD_INPUT_HANDLE, windows.STD_OUTPUT_HANDLE} {
		h, e := windows.GetStdHandle(std)
		var mode uint32
		if e != nil || windows.GetConsoleMode(h, &mode) != nil {
			console = false
		}
	}
	b, _ := json.Marshal(struct {
		Args            []string
		Home            string
		Console         bool
		ColorSuppressed bool
	}{os.Args[3:], os.Getenv("CODEX_HOME"), console, os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"})
	if os.WriteFile(os.Args[2], b, 0600) != nil {
		os.Exit(9)
	}
	_, _ = os.Stdout.WriteString("Caelis owned terminal preview acceptance\r\n")
	time.Sleep(time.Minute)
	os.Exit(0)
}

func TestConsoleEnvironmentKeepsActualTerminalCapabilities(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")
	block, err := consoleEnvironment(nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(utf16.Decode(block))
	if !strings.Contains(text, "TERM=xterm-256color\x00") || !strings.Contains(text, "COLORTERM=truecolor\x00") {
		t.Fatal("terminal capabilities were overwritten")
	}
}

func TestWindowsConsoleClaimsOncePreservesUnicodeArgvAndOwnsOnlyHandles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "中文 path ' quoted")
	if e := os.Mkdir(root, 0700); e != nil {
		t.Fatal(e)
	}
	if e := localstate.ProtectDirectory(root); e != nil {
		t.Fatal(e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	evidence := filepath.Join(root, "result.json")
	pending := filepath.Join(root, "pending")
	path := filepath.Join(root, "launch.command")
	prefix := filepath.Join(root, "accepted-")
	args := []string{"--caelis-terminal-fixture", evidence, "a b", "汉字", "quote\"value", `C:\目录 with space\`, "'literal'"}
	v := windowsLaunch{Version: 1, Command: LaunchArguments{Binary: exe, Directory: root, Args: args, Env: map[string]string{"CODEX_HOME": root}}, Pending: pending, AcceptedPrefix: prefix}
	if e := localstate.Write(path, v); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(pending, []byte("original-receipt"), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	window, e := LaunchConsole(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	w := window.(*consoleWindow)
	defer w.Release()
	h, e := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(w.pid))
	if e != nil {
		t.Fatal(e)
	}
	defer windows.CloseHandle(h)
	defer windows.TerminateProcess(h, 1)
	var result struct {
		Args    []string
		Home    string
		Console bool
	}
	for result.Args == nil {
		b, _ := os.ReadFile(evidence)
		_ = json.Unmarshal(b, &result)
		if result.Args != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("console fixture did not execute")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !result.Console || result.Home != root || len(result.Args) != len(args)-2 {
		t.Fatal("native console/environment missing", result)
	}
	for i, a := range args[2:] {
		if result.Args[i] != a {
			t.Fatal("argv changed", i, result.Args)
		}
	}
	if b, e := os.ReadFile(prefix + strconv.Itoa(w.pid)); e != nil || string(b) != "original-receipt" {
		t.Fatal("receipt not bound to exact process", e)
	}
	if _, e := LaunchConsole(ctx, path); e == nil || !errors.Is(e, ErrLaunchNotSubmitted) {
		t.Fatal("claimed launch replayed", e)
	}
	if e := w.Confirm(ctx, w.pid+1); !errors.Is(e, ErrWindowIdentity) {
		t.Fatal("foreign owner confirmed", e)
	}
	identity, err := os.ReadFile(consoleIdentityPath(path, w.pid))
	if err != nil {
		t.Fatal(err)
	}
	var recorded consoleIdentity
	if json.Unmarshal(identity, &recorded) != nil || recorded.PID != w.pid || recorded.Created == 0 {
		t.Fatal("original creation identity lost after duplicate claim")
	}
	if _, err = w.Thumbnail(ctx); err == nil {
		t.Log("Owned native console thumbnail captured")
	} else {
		t.Log("Host has no attestable rendered thumbnail:", err)
	}
	w.Release()
	if n, e := windows.WaitForSingleObject(h, 0); e != nil || n != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("release killed independent client", n, e)
	}
}

func TestConsoleEnvironmentDropsCallerIdentityAndRejectsUnknownOverrides(t *testing.T) {
	t.Setenv("codex_thread_id", "foreign-owner")
	t.Setenv("Caelis_Control_Token", "PRIVATE_SENTINEL")
	t.Setenv("no_color", "1")
	t.Setenv("TERM", "dumb")
	block, e := consoleEnvironment(map[string]string{"CODEX_HOME": t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range strings.Split(string(utf16.Decode(block)), "\x00") {
		key, _, _ := strings.Cut(part, "=")
		if strings.EqualFold(key, "CODEX_THREAD_ID") || strings.HasPrefix(strings.ToUpper(key), "CAELIS_CONTROL_") || strings.EqualFold(key, "NO_COLOR") || strings.EqualFold(key, "TERM") {
			t.Fatal("caller authority inherited")
		}
	}
	if _, e := consoleEnvironment(map[string]string{"PATH": "changed"}); !errors.Is(e, ErrWindowIdentity) {
		t.Fatal("unowned environment override accepted")
	}
}
