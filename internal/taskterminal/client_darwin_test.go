//go:build darwin && cgo

package taskterminal

import (
	"os/exec"
	"testing"
)

func TestTerminalClientLifetimeAndPIDReuse(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	pid := cmd.Process.Pid
	state, birth := terminalClientState(pid, 0)
	if state != 1 || birth == 0 {
		t.Fatal("live client unrecognized", state, birth)
	}
	if state, _ := terminalClientState(pid, birth); state != 1 {
		t.Fatal("same client", state)
	}
	if state, _ := terminalClientState(pid, birth+1); state != 0 {
		t.Fatal("reused PID treated as original client", state)
	}
	if state, _ := terminalClientState(-1, 0); state != -1 {
		t.Fatal("invalid identity treated as exit", state)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if state, _ := terminalClientState(pid, birth); state != 0 {
		t.Fatal("exited client still alive", state)
	}
}
