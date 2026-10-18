package taskterminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
	"golang.org/x/sys/windows"
)

type updateObserverFixture struct{ closed int }

func (*updateObserverFixture) Confirm(context.Context, int) error           { return nil }
func (*updateObserverFixture) Release()                                     {}
func (w *updateObserverFixture) CloseClientForUpdate(context.Context) error { w.closed++; return nil }

func TestWindowsUpdateClosesOriginalClientWithoutDroppingBinding(t *testing.T) {
	window := &updateObserverFixture{}
	launcher := NewManaged(t.TempDir(), nil)
	launcher.windows["original"] = window
	manager := NewWindowManager(t.TempDir(), nil, nil, nil, nil)
	manager.entries["original"] = &managedEntry{controller: &windowController{}, launcher: launcher}
	if err := manager.CloseClientsForUpdate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if window.closed != 1 || manager.entries["original"].launcher.windows["original"] != window {
		t.Fatal("update replaced or removed the original binding")
	}
}

func TestWindowsUpdateTerminatesOnlyTheOriginalObserverProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	start := func(name string) *exec.Cmd {
		receipt := filepath.Join(t.TempDir(), name+".json")
		command := exec.Command(exe, "--caelis-terminal-fixture", receipt)
		runtimeenv.BackgroundCommand(command)
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { command.Process.Kill(); command.Wait() })
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(receipt); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("process did not start")
			}
			time.Sleep(10 * time.Millisecond)
		}
		return command
	}
	observer, host, foreign := start("observer"), start("host"), start("foreign-observer")
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(observer.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	window := &consoleWindow{process: handle, pid: observer.Process.Pid, observer: true}
	if err := window.CloseClientForUpdate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if state, err := windows.WaitForSingleObject(handle, 0); err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatal("owned observer still running", err)
	}
	for _, command := range []*exec.Cmd{host, foreign} {
		h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(command.Process.Pid))
		if err != nil {
			t.Fatal(err)
		}
		state, err := windows.WaitForSingleObject(h, 0)
		windows.CloseHandle(h)
		if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
			t.Fatal("unowned process was ended", err)
		}
	}
}
