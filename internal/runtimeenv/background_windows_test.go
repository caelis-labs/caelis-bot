package runtimeenv

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsBackgroundProbeChild(t *testing.T) {
	if os.Getenv("CAELIS_BOT_BACKGROUND_FIXTURE") != "1" {
		return
	}
	hwnd, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
	if hwnd != 0 {
		os.Exit(2)
	}
	fmt.Print("no-console")
	os.Exit(0)
}

func TestWindowsBackgroundProbeHasNoConsole(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), path, "-test.run=^TestWindowsBackgroundProbeChild$")
	cmd.Env = append(Clean(os.Environ()), "CAELIS_BOT_BACKGROUND_FIXTURE=1")
	BackgroundCommand(cmd)
	out, err := cmd.Output()
	if err != nil || string(out) != "no-console" {
		t.Fatalf("background console suppression failed: %v", err)
	}
}
