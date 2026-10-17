package windowsupdate

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

func TestWindowsUpdateGuardsThenClosesOnlyItsExactConsoleHelper(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(root, "caelis-terminal.exe")
	if err := os.WriteFile(helper, data, 0600); err != nil {
		t.Fatal(err)
	}
	name, _ := windows.UTF16PtrFromString(helper)
	si := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(name, nil, nil, nil, false, windows.CREATE_SUSPENDED|windows.CREATE_NO_WINDOW, nil, nil, &si, &pi); err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(pi.Thread)
	defer windows.CloseHandle(pi.Process)
	defer func() {
		_ = windows.TerminateProcess(pi.Process, 0)
		_, _ = windows.WaitForSingleObject(pi.Process, 5000)
	}()
	if TerminalIdle(root) == nil {
		t.Fatal("mapped console helper accepted for update")
	}
	if n, err := windows.WaitForSingleObject(pi.Process, 0); err != nil || n != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("guard ended independent client", err)
	}
	if err := TerminalIdle(t.TempDir()); err != nil {
		t.Fatal("unrelated installation blocked", err)
	}
	foreignRoot := t.TempDir()
	foreignPath := filepath.Join(foreignRoot, "caelis-terminal.exe")
	if err := os.WriteFile(foreignPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	foreignName, _ := windows.UTF16PtrFromString(foreignPath)
	var foreign windows.ProcessInformation
	if err := windows.CreateProcess(foreignName, nil, nil, nil, false, windows.CREATE_SUSPENDED|windows.CREATE_NO_WINDOW, nil, nil, &si, &foreign); err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(foreign.Thread)
	defer windows.CloseHandle(foreign.Process)
	defer func() {
		_ = windows.TerminateProcess(foreign.Process, 0)
		_, _ = windows.WaitForSingleObject(foreign.Process, 5000)
	}()
	if err := CloseTerminalHelpers(t.Context(), root); err != nil {
		t.Fatal("authorized helper closure failed", err)
	}
	if n, err := windows.WaitForSingleObject(foreign.Process, 0); err != nil || n != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("another installation's same-name helper was stopped", err)
	}
	if err := TerminalIdle(root); err != nil {
		t.Fatal("ended client still blocks update", err)
	}
}
