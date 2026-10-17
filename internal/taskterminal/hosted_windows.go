package taskterminal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type consoleIdentity struct {
	PID     int    `json:"pid"`
	Created uint64 `json:"created"`
}

func processBirth(handle windows.Handle) (uint64, error) {
	var created, exited, kernel, user windows.Filetime
	err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user)
	return uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime), err
}
func consoleIdentityPath(path string, pid int) string {
	return filepath.Join(filepath.Dir(path), "process-identity-"+strconv.Itoa(pid)+".json")
}
func writeConsoleIdentity(path string, handle windows.Handle, pid int) error {
	birth, err := processBirth(handle)
	if err != nil {
		return err
	}
	return localstate.Write(consoleIdentityPath(path, pid), consoleIdentity{PID: pid, Created: birth})
}

func WindowsTemplateArgs(command, executable, path string) ([]string, error) {
	if len(command) > 4096 || strings.ContainsRune(command, 0) {
		return nil, errors.New("invalid Windows terminal command")
	}
	// CommandLineToArgvW tolerates an unmatched quote. A saved user template
	// should instead report the incomplete edit before it becomes executable.
	quoted, slashes := false, 0
	for _, ch := range command {
		if ch == '\\' {
			slashes++
			continue
		}
		if ch == '"' && slashes%2 == 0 {
			quoted = !quoted
		}
		slashes = 0
	}
	if quoted {
		return nil, errors.New("unfinished custom terminal quoting")
	}
	args, err := windows.DecomposeCommandLine(command)
	if err != nil || len(args) < 2 || args[0] == "" {
		return nil, errors.New("terminal requires executable and {script}")
	}
	count := 0
	var out []string
	for i, arg := range args {
		if strings.Contains(arg, "{script}") {
			if i == 0 || arg != "{script}" {
				return nil, errors.New("{script} must be a complete argument")
			}
			count++
			out = append(out, executable, "--task-terminal-client", path)
		} else {
			out = append(out, arg)
		}
	}
	if count != 1 {
		return nil, errors.New("terminal requires one {script} argument")
	}
	return out, nil
}
func ValidateCustomTemplate(command string) bool {
	_, err := WindowsTemplateArgs(command, `C:\Bot\caelis-bot.exe`, `C:\Bot\launch.command`)
	return err == nil
}

type hostedConsole struct {
	consoleWindow
	descriptor string
	binary     string
}

// Host CLIs may return before a tab starts. The original descriptor and receipt
// remain the only connection authority; an uncertain host never triggers retry.
func LaunchHostedConsole(ctx context.Context, command, path string) (Window, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, NotLaunched(err)
	}
	client := filepath.Join(filepath.Dir(executable), "caelis-terminal.exe")
	if info, err := os.Stat(client); err != nil || !info.Mode().IsRegular() {
		return nil, NotLaunched(ErrWindowUnsupported)
	}
	return launchHostedConsole(ctx, command, path, client)
}

func launchHostedConsole(ctx context.Context, command, path, client string) (Window, error) {
	if ctx.Err() != nil {
		return nil, NotLaunched(ctx.Err())
	}
	args, err := WindowsTemplateArgs(command, client, path)
	if err != nil {
		return nil, NotLaunched(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 65536 {
		return nil, NotLaunched(ErrWindowIdentity)
	}
	var launch windowsLaunch
	if json.Unmarshal(data, &launch) != nil || launch.Version != 1 || !filepath.IsAbs(launch.Command.Binary) || !filepath.IsAbs(launch.Command.Directory) || filepath.Clean(launch.Pending) != filepath.Join(filepath.Dir(path), "pending") || launch.AcceptedPrefix != filepath.Join(filepath.Dir(path), "accepted-") {
		return nil, NotLaunched(ErrWindowIdentity)
	}
	host, err := exec.LookPath(args[0])
	if err != nil || !strings.EqualFold(filepath.Ext(host), ".exe") {
		return nil, NotLaunched(ErrWindowUnsupported)
	}
	cmd := exec.Command(host, args[1:]...)
	cmd.Env = runtimeenv.Clean(os.Environ())
	if err = cmd.Start(); err != nil {
		return nil, NotLaunched(err)
	}
	go func() { _ = cmd.Wait() }()
	return &hostedConsole{consoleWindow: consoleWindow{observer: launch.Observer}, descriptor: path, binary: launch.Command.Binary}, nil
}
func (w *hostedConsole) Confirm(ctx context.Context, pid int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if pid <= 0 {
		return ErrWindowIdentity
	}
	if w.process != 0 {
		if pid != w.pid {
			return ErrWindowIdentity
		}
		return nil
	}
	data, err := os.ReadFile(consoleIdentityPath(w.descriptor, pid))
	if err != nil || len(data) > 512 {
		return ErrWindowIdentity
	}
	var identity consoleIdentity
	if json.Unmarshal(data, &identity) != nil || identity.PID != pid || identity.Created == 0 {
		return ErrWindowIdentity
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ErrWindowIdentity
	}
	birth, err := processBirth(handle)
	var name [32768]uint16
	n := uint32(len(name))
	query := windows.QueryFullProcessImageName(handle, 0, &name[0], &n)
	if err != nil || birth != identity.Created || query != nil || !strings.EqualFold(filepath.Clean(windows.UTF16ToString(name[:n])), filepath.Clean(w.binary)) {
		windows.CloseHandle(handle)
		return ErrWindowIdentity
	}
	w.pid, w.process, w.hwnd = pid, handle, consoleHandle(uint32(pid))
	return nil
}

// This GUI-subsystem entry joins only its invoking host's console. It starts a
// native child with real stdio, claims once, then waits without owning its Job.
func RunClient(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "--task-terminal-client" {
		return false, nil
	}
	if len(args) != 2 || !filepath.IsAbs(args[1]) {
		return true, ErrWindowIdentity
	}
	if hwnd, _, _ := terminalKernel.NewProc("GetConsoleWindow").Call(); hwnd == 0 {
		if ok, _, _ := terminalKernel.NewProc("AttachConsole").Call(uintptr(uint32(0xffffffff))); ok == 0 {
			return true, ErrWindowUnsupported
		}
	}
	window, err := launchConsole(context.Background(), args[1], false)
	if err != nil {
		return true, err
	}
	w := window.(*consoleWindow)
	defer w.Release()
	for {
		n, e := windows.WaitForSingleObject(w.process, uint32(time.Second/time.Millisecond))
		if e != nil {
			return true, e
		}
		if n == windows.WAIT_OBJECT_0 {
			return true, nil
		}
	}
}

func WindowsChoices() []Choice {
	_, err := exec.LookPath("wt.exe")
	return []Choice{{ID: "system", Name: "Windows Console", Available: true}, {ID: "windows-console", Name: "Windows Console Host", Available: true}, {ID: "windows-terminal", Name: "Windows Terminal", Available: err == nil}}
}

func LaunchClassicConsole(ctx context.Context, path string) (Window, error) {
	directory, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, NotLaunched(err)
	}
	return LaunchHostedConsole(ctx, `"`+filepath.Join(directory, "conhost.exe")+`" {script}`, path)
}
