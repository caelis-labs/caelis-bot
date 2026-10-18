package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func init() {
	path := os.Getenv("CAELIS_BOT_SCOPE_FIXTURE")
	if path == "" || len(os.Args) < 2 {
		return
	}
	switch os.Args[1] {
	case "app-server":
		exe, _ := os.Executable()
		child := exec.Command(exe, "caelis-owned-child")
		if child.Start() != nil {
			os.Exit(7)
		}
		b, _ := json.Marshal([]int{os.Getpid(), child.Process.Pid})
		if os.WriteFile(path, b, 0600) != nil {
			os.Exit(8)
		}
	case "caelis-owned-child", "caelis-unrelated-child":
	default:
		return
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}

func TestWindowsJobClosesItsDescendantsAndLeavesUnrelatedProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "owned.json")
	t.Setenv("CAELIS_BOT_SCOPE_FIXTURE", path)
	unrelated := exec.Command(exe, "caelis-unrelated-child")
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	conn, stop, err := startProcess(ctx, Options{Binary: exe, Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer stop()
	var pids []int
	for len(pids) != 2 {
		b, _ := os.ReadFile(path)
		_ = json.Unmarshal(b, &pids)
		if len(pids) == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("owned child readiness not confirmed")
		case <-time.After(20 * time.Millisecond):
		}
	}
	var handles []windows.Handle
	for _, pid := range append(pids, unrelated.Process.Pid) {
		h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if err != nil {
			t.Fatal(err)
		}
		defer windows.CloseHandle(h)
		handles = append(handles, h)
	}
	stop()
	for _, h := range handles[:2] {
		if n, err := windows.WaitForSingleObject(h, 5000); err != nil || n != windows.WAIT_OBJECT_0 {
			t.Fatal("owned descendant survived scope close", n, err)
		}
	}
	if n, err := windows.WaitForSingleObject(handles[2], 0); err != nil || n != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("unrelated process was affected", n, err)
	}
}
