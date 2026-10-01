//go:build (darwin && cgo) || linux

package codex

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestSupervisorKnownStopFaultSurvivesExactNativeFallback(t *testing.T) {
	child := exec.Command("/bin/sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	p, server := supervisorFaultFixture(t, "native-stop-unconfirmed", false)
	p.tools = newOwnedTools(child.Process.Pid)
	defer p.tools.terminate()
	p.tools.capture()
	if err := p.tools.failure(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	err := p.Stop(ctx)
	if serverErr := <-server; serverErr != nil {
		t.Fatal(serverErr)
	}
	if !errors.Is(err, errWatchdogStopUnconfirmed) {
		t.Fatal("OS fallback erased known native stop failure", err)
	}
	if repeated := p.Stop(t.Context()); repeated != err {
		t.Fatal("Stop replaced original native proof failure", repeated, err)
	}
	live, readErr := p.tools.watchdogRootLive()
	if live || readErr != nil {
		t.Fatal("known failure prevented exact owned fallback cleanup", live, readErr)
	}
}
