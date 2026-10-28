//go:build !windows

package sshtransport

import (
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func processAlive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

func waitExit(t *testing.T, c *tunnel) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(4 * time.Second):
		t.Fatal("owned channel did not finish cleanup")
	}
}

// Hold cancellation delivery, leaving the real owned child alive until release.
// This forces the otherwise brief interval between cancellation and cleanup,
// without timing sleeps or adding hooks to production code.
func holdChannelExit(t *testing.T, v *Transport, c *tunnel) (cancelled <-chan struct{}, release func()) {
	t.Helper()
	requested := make(chan struct{})
	var once sync.Once
	v.mu.Lock()
	cancel := c.cancel
	c.cancel = func() { once.Do(func() { close(requested) }) }
	v.mu.Unlock()
	t.Cleanup(func() { cancel(); waitExit(t, c) })
	return requested, cancel
}

func exerciseExitBarrier(t *testing.T, v *Transport, address, action string) {
	t.Helper()
	old := waitChannel(t, v, address, "ready")
	target, _ := Target(address)
	cancelled, release := holdChannelExit(t, v, old)
	if action == "cancel" {
		// Controlled prompt, actual owned process. Native trust/password/key
		// authentication itself is covered by TestNativeOpenSSH*.
		v.mu.Lock()
		old.state = "authentication"
		old.prompt = &api.SSHPrompt{ID: "cancelled-fixture-prompt"}
		v.mu.Unlock()
		if err := v.Answer(api.SSHAnswer{Target: &target, ID: "cancelled-fixture-prompt", Cancel: true}); err != nil {
			t.Fatal(err)
		}
	} else {
		v.ReleaseUnused([]string{address})
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("cancellation was not requested")
	}
	if !processAlive(old.pid) {
		t.Fatal("fixture did not retain the old process through cleanup")
	}
	s, err := v.Start(target)
	v.mu.Lock()
	current := v.tunnels[address]
	v.mu.Unlock()
	if current != old {
		current = waitChannel(t, v, address, "ready")
		t.Fatalf("same-target generations overlap: old PID %d alive=%t; new PID %d alive=%t", old.pid, processAlive(old.pid), current.pid, processAlive(current.pid))
	}
	if err == nil || err.Error() != "ssh_stopping" || s.State != "failed" || s.Issue != "ssh_stopping" || s.Prompt != nil {
		t.Fatalf("Start reused cancelled generation instead of reporting stopping: state=%s issue=%s prompt=%t err=%v", s.State, s.Issue, s.Prompt != nil, err)
	}
	if s := v.Status(target); s.Issue != "ssh_stopping" || s.Prompt != nil {
		t.Fatal("status exposed cancelled authentication/ready state", s)
	}
	if _, err := v.Resolve(t.Context(), address); err == nil || err.Error() != "ssh_stopped" {
		t.Fatal("health read crossed the stopping barrier", err)
	}
	// Cleanup cannot block an independent target or forget its ownership.
	other := "ssh://independent-barrier-fixture:" + strconv.Itoa(target.Port)
	if _, err := v.Resolve(t.Context(), other); err != nil {
		t.Fatal("stopping target blocked another channel", err)
	}
	independent := waitChannel(t, v, other, "ready")
	assertConcurrentStart := func(wantIssue string) {
		t.Helper()
		var wg sync.WaitGroup
		for range 24 {
			wg.Go(func() {
				s, err := v.Start(target)
				if s.Issue != wantIssue || (wantIssue == "" && err != nil) || (wantIssue != "" && (err == nil || err.Error() != wantIssue)) {
					t.Errorf("concurrent explicit Start: issue=%s err=%v", s.Issue, err)
				}
			})
		}
		wg.Wait()
	}
	assertConcurrentStart("ssh_stopping")
	release()
	waitExit(t, old)
	if processAlive(old.pid) {
		t.Fatal("completion published before child exit")
	}
	v.mu.Lock()
	generation := v.generation
	_, retained := v.tunnels[address]
	v.mu.Unlock()
	if action == "release" && retained {
		t.Fatal("unused channel retained after cleanup")
	}
	assertConcurrentStart("")
	next := waitChannel(t, v, address, "ready")
	v.mu.Lock()
	defer v.mu.Unlock()
	if next == old || next.pid == old.pid || v.generation != generation+1 || !processAlive(next.pid) || processAlive(old.pid) {
		t.Fatal("explicit reconnect did not produce exactly one successor after exit")
	}
	if v.tunnels[other] != independent || !processAlive(independent.pid) {
		t.Fatal("reconnecting target disturbed another owned channel")
	}
	t.Logf("%s: 25 immediate Starts reported stopping; old PID %d exited before successor PID %d; 24 concurrent Starts created one successor", action, old.pid, next.pid)
}

func TestCancelThenImmediateStartWaitsForExit(t *testing.T) {
	v, set, address := reconnectProcess(t)
	set("ready")
	if _, err := v.Resolve(t.Context(), address); err != nil {
		t.Fatal(err)
	}
	exerciseExitBarrier(t, v, address, "cancel")
}

func TestReleaseThenImmediateStartNeverOverlaps(t *testing.T) {
	v, set, address := reconnectProcess(t)
	set("ready")
	if _, err := v.Resolve(t.Context(), address); err != nil {
		t.Fatal(err)
	}
	exerciseExitBarrier(t, v, address, "release")
}
