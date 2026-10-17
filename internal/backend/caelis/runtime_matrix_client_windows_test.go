package caelis

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"golang.org/x/sys/windows"
)

// This runs a released, unchanged TUI in a private ConPTY. It never imports
// Core internals, sends a prompt, disables sandboxing or touches the user's Store.
func observeMatrixClient(t *testing.T, ctx context.Context, host *client, binary, cwd, store, session string) func(string) {
	t.Helper()
	var status wire.StatusSnapshot
	if err := host.json(ctx, "GET", "/status?cwd="+url.QueryEscape(cwd)+"&workspace_key="+url.QueryEscape(cwd), nil, &status, "", ""); err != nil {
		t.Fatal(err)
	}
	op := "matrix-observer-untrusted-workspace"
	var result wire.CommandResult
	request := wire.WorkspaceTrustRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Cwd: cwd, WorkspaceKey: cwd, TrustLevel: wire.WorkspaceTrustLevelUntrusted}
	if err := host.json(ctx, "POST", "/configuration/workspace-trust", request, &result, op, string(status.Configuration.Revision)); err != nil || !succeeded(result.Outcome) {
		t.Fatalf("observer workspace: %v %+v", err, result)
	}
	launch, err := taskterminal.TerminalArguments(api.TerminalTarget{Runtime: "caelis", Binary: binary, Directory: cwd, Endpoint: host.origin, Session: session, Store: store, TokenFile: filepath.Join(store, "runtime", "service", "auth.token")})
	if err != nil {
		t.Fatal(err)
	}
	processes := []*matrixConsole{
		startMatrixConsole(t, binary, []string{"--interactive", "--no-animation", "--session", session, "--store-dir", store}, cwd),
		startMatrixConsole(t, binary, launch.Args, launch.Directory),
	}
	return func(marker string) {
		t.Helper()
		for _, process := range processes {
			deadline := time.NewTimer(40 * time.Second)
			defer deadline.Stop()
			for {
				state, err := windows.WaitForSingleObject(process.process, 0)
				if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
					t.Fatalf("original released TUI exited: %v\n%s", err, process.text())
				}
				if strings.Contains(process.text(), marker) {
					t.Logf("same released TUI PID %d observed %s", process.pid, marker)
					break
				}
				select {
				case <-ctx.Done():
					t.Fatalf("TUI observation cancelled: %v", ctx.Err())
				case <-deadline.C:
					t.Fatalf("TUI did not observe %s automatically:\n%s", marker, process.text())
				case <-time.After(50 * time.Millisecond):
				}
			}
		}
	}
}

type matrixConsole struct {
	process windows.Handle
	pid     uint32
	mu      sync.Mutex
	output  []byte
}

var matrixANSI = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\)|[()][A-Z0-9]|[=>])`)

func (p *matrixConsole) text() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return matrixANSI.ReplaceAllString(string(p.output), "")
}

func startMatrixConsole(t *testing.T, binary string, args []string, cwd string) *matrixConsole {
	t.Helper()
	var inputRead, inputWrite, outputRead, outputWrite windows.Handle
	if err := windows.CreatePipe(&inputRead, &inputWrite, nil, 0); err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(inputRead)
	t.Cleanup(func() { windows.CloseHandle(inputWrite) })
	if err := windows.CreatePipe(&outputRead, &outputWrite, nil, 0); err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(outputWrite)
	output := os.NewFile(uintptr(outputRead), "matrix-console-output")
	t.Cleanup(func() { output.Close() })
	var console windows.Handle
	if err := windows.CreatePseudoConsole(windows.Coord{X: 160, Y: 60}, inputRead, outputWrite, 0, &console); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { windows.ClosePseudoConsole(console) })
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		t.Fatal(err)
	}
	defer attributes.Delete()
	if err = attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&console)), unsafe.Sizeof(console)); err != nil {
		t.Fatal(err)
	}
	line, err := windows.UTF16FromString(windows.ComposeCommandLine(append([]string{binary}, args...)))
	if err != nil {
		t.Fatal(err)
	}
	exe, err := windows.UTF16PtrFromString(binary)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := windows.UTF16PtrFromString(cwd)
	if err != nil {
		t.Fatal(err)
	}
	// The containing matrix child already has an isolated OS-only environment.
	env := utf16.Encode([]rune(strings.Join(append(os.Environ(), "TERM=xterm-256color"), "\x00") + "\x00\x00"))
	startup := windows.StartupInfoEx{}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Flags = windows.STARTF_USESTDHANDLES
	startup.StdInput = windows.InvalidHandle
	startup.StdOutput = windows.InvalidHandle
	startup.StdErr = windows.InvalidHandle
	startup.ProcThreadAttributeList = attributes.List()
	var info windows.ProcessInformation
	if err = windows.CreateProcess(exe, &line[0], nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT, &env[0], directory, &startup.StartupInfo, &info); err != nil {
		t.Fatal(err)
	}
	windows.CloseHandle(info.Thread)
	p := &matrixConsole{process: info.Process, pid: info.ProcessId}
	// Only terminate this test-created observer. Never kill a Host or a tree.
	t.Cleanup(func() {
		windows.TerminateProcess(p.process, 0)
		windows.WaitForSingleObject(p.process, 5000)
		windows.CloseHandle(p.process)
	})
	go func() {
		buffer := make([]byte, 8192)
		for {
			n, err := output.Read(buffer)
			if n > 0 {
				p.mu.Lock()
				p.output = append(p.output, buffer[:n]...)
				if len(p.output) > 2<<20 {
					p.output = append([]byte(nil), p.output[len(p.output)-(1<<20):]...)
				}
				p.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	return p
}
