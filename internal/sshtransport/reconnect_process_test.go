//go:build !windows

package sshtransport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

// Exercises the actual process/control lifecycle with deterministic failures and
// a virtual retry clock. Real OpenSSH coverage lives in reconnect_native_test.
func reconnectProcess(t *testing.T) (*Transport, func(string), string) {
	t.Helper()
	v := fixtureTransport(t)
	mode := filepath.Join(t.TempDir(), "mode")
	v.binary = filepath.Join(t.TempDir(), "ssh")
	put(t, v.binary, fmt.Sprintf(`#!/bin/sh
fixture_mode=$(cat %q)
case "$*" in *' -O '*) test "$fixture_mode" = ready; exit $?;; esac
case "$fixture_mode" in
 offline) echo 'Connection refused' >&2; exit 255;;
 auth) echo 'Permission denied (publickey,password)' >&2; exit 255;;
 trust) echo 'REMOTE HOST IDENTIFICATION HAS CHANGED!' >&2; exit 255;;
 config) echo 'Bad configuration option' >&2; exit 255;;
esac
exec /bin/sleep 60
`, mode))
	if err := os.Chmod(v.binary, 0700); err != nil {
		t.Fatal(err)
	}
	set := func(value string) { put(t, mode, value) }
	set("offline")
	return v, set, "ssh://private-fixture-host:9317"
}

func advanceRetry(t *Transport) {
	t.mu.Lock()
	now := t.now().Add(time.Hour)
	t.now = func() time.Time { return now }
	t.mu.Unlock()
}

func waitChannel(t *testing.T, v *Transport, address, state string) *tunnel {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		v.mu.Lock()
		c := v.tunnels[address]
		ok := c != nil && c.state == state
		v.mu.Unlock()
		if ok {
			return c
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("channel did not become %s", state)
	return nil
}

func disconnectChannel(t *testing.T, v *Transport, address string) {
	t.Helper()
	v.mu.Lock()
	pid := v.tunnels[address].pid
	v.mu.Unlock()
	p, err := os.FindProcess(pid)
	if err != nil || p.Kill() != nil {
		t.Fatal("kill isolated channel", err)
	}
	waitChannel(t, v, address, "failed")
}

func concurrentResolve(t *testing.T, v *Transport, address string) {
	t.Helper()
	v.mu.Lock()
	generation := v.generation
	v.mu.Unlock()
	var wg sync.WaitGroup
	addresses := make(chan string, 24)
	for range 24 {
		wg.Go(func() {
			a, err := v.Resolve(t.Context(), address)
			if err != nil {
				t.Error("concurrent recovery", err)
			}
			addresses <- a
		})
	}
	wg.Wait()
	close(addresses)
	first := <-addresses
	for a := range addresses {
		if a != first || a == "" {
			t.Fatal("duplicate forward returned")
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.generation != generation+1 || v.tunnels[address].failures != 0 {
		t.Fatal("concurrent reads created multiple channels or retained a lifetime failure budget")
	}
}

func TestReconnectOutlastsOldBudgetAndRepeatedCycles(t *testing.T) {
	v, set, address := reconnectProcess(t)
	var events []diagnosticlog.Record
	var eventsMu sync.Mutex
	v.SetDiagnostics(func(r diagnosticlog.Record) { eventsMu.Lock(); defer eventsMu.Unlock(); events = append(events, r) })
	for i := 1; i <= 7; i++ {
		advanceRetry(v)
		if _, err := v.Resolve(t.Context(), address); err == nil {
			t.Fatal("offline channel unexpectedly ready")
		}
		c := waitChannel(t, v, address, "failed")
		v.mu.Lock()
		failures, generation := c.failures, v.generation
		v.mu.Unlock()
		if failures != i {
			t.Fatalf("stopped retrying at failure %d: %d", i, failures)
		}
		for range 5 {
			v.Resolve(t.Context(), address)
		}
		v.mu.Lock()
		changed := v.generation != generation
		v.mu.Unlock()
		if changed {
			t.Fatal("health polling bypassed backoff")
		}
	}
	set("ready")
	advanceRetry(v)
	concurrentResolve(t, v, address)
	for range 6 {
		disconnectChannel(t, v, address)
		advanceRetry(v)
		concurrentResolve(t, v, address)
	}
	target, _ := Target(address)
	if err := v.Stop(target); err != nil {
		t.Fatal(err)
	}
	advanceRetry(v)
	v.mu.Lock()
	generation := v.generation
	v.mu.Unlock()
	for range 6 {
		if _, err := v.Resolve(t.Context(), address); err == nil {
			t.Fatal("explicit stop revived")
		}
	}
	v.mu.Lock()
	if v.generation != generation {
		t.Error("explicit stop created another generation")
	}
	v.mu.Unlock()
	// Explicit user action alone can cross the stop boundary.
	if _, err := v.Start(target); err != nil {
		t.Fatal(err)
	}
	waitChannel(t, v, address, "ready")
	v.Close()
	if _, err := v.Resolve(t.Context(), address); err == nil {
		t.Fatal("closed transport revived")
	}
	eventsMu.Lock()
	defer eventsMu.Unlock()
	encoded, _ := json.Marshal(events)
	if strings.Contains(string(encoded), "private-fixture-host") || strings.Contains(string(encoded), v.root) || len(events) > 65 {
		t.Fatal("diagnostics exposed private data or logged every poll")
	}
	var recovered, exited bool
	for _, r := range events {
		recovered = recovered || r.Phase == "ready" && r.ConsecutiveFailures == 7
		exited = exited || r.Phase == "retry_wait" && r.ExitCode != nil && r.ProcessID > 0 && r.RetryAfterMS > 0
	}
	if !recovered || !exited {
		t.Fatal("diagnostics lost recovery/exit correlation")
	}
}

func TestReconnectRequiresExplicitAuthenticationAndPreservesOtherChannels(t *testing.T) {
	for _, mode := range []string{"auth", "trust", "config"} {
		t.Run(mode, func(t *testing.T) {
			v, set, address := reconnectProcess(t)
			set(mode)
			v.Resolve(t.Context(), address)
			failed := waitChannel(t, v, address, "failed")
			if retryable(failed.issue) {
				t.Fatalf("fixture failure unexpectedly retryable: %s", failed.issue)
			}
			set("ready")
			for range 6 {
				advanceRetry(v)
				v.Resolve(t.Context(), address)
			}
			v.mu.Lock()
			generation := v.generation
			v.mu.Unlock()
			if generation != 1 {
				t.Fatalf("automatic retry crossed authentication/config boundary: generation=%d status=%+v", generation, v.Status(api.SSHTarget{Host: "private-fixture-host", Port: 9317}))
			}
			if _, err := v.Resolve(t.Context(), "ssh://another-fixture:9317"); err != nil {
				t.Fatal("failed connection blocked another target", err)
			}
			target, _ := Target(address)
			v.Start(target)
			waitChannel(t, v, address, "ready")
			v.Stop(target)
			if _, err := v.Resolve(t.Context(), "ssh://another-fixture:9317"); err != nil {
				t.Fatal("stop crossed connection boundary", err)
			}
		})
	}
}

func TestReconnectDoesNotForgetStoppedChannelsAtCapacity(t *testing.T) {
	v, set, address := reconnectProcess(t)
	set("ready")
	if _, err := v.Resolve(t.Context(), address); err != nil {
		t.Fatal(err)
	}
	// Cancel an actual owned process through the same prompt-answer boundary.
	v.mu.Lock()
	c := v.tunnels[address]
	c.prompt = &api.SSHPrompt{ID: "fixture-prompt"}
	c.state = "authentication"
	v.mu.Unlock()
	target, _ := Target(address)
	if err := v.Answer(api.SSHAnswer{Target: &target, ID: "fixture-prompt", Cancel: true}); err != nil {
		t.Fatal(err)
	}
	waitChannel(t, v, address, "failed")
	for i := range 15 {
		if _, err := v.Resolve(t.Context(), fmt.Sprintf("ssh://other-%d:9317", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := v.Start(api.SSHTarget{Host: "seventeenth", Port: 9317}); err == nil || err.Error() != "ssh_channel_limit" {
		t.Fatal("capacity eviction forgot a stop boundary", err)
	}
	advanceRetry(v)
	if _, err := v.Resolve(t.Context(), address); err == nil {
		t.Fatal("cancelled authentication revived after capacity pressure")
	}
	v.ReleaseUnused([]string{address})
	if _, err := v.Resolve(t.Context(), "ssh://seventeenth:9317"); err != nil {
		t.Fatal("unreferenced stop entry could not be reclaimed", err)
	}
}

func TestReconnectStopFencesAnInFlightChannelStart(t *testing.T) {
	v, set, address := reconnectProcess(t)
	set("ready")
	v.configMu.Lock()
	resolved := make(chan error, 1)
	go func() { _, err := v.Resolve(t.Context(), address); resolved <- err }()
	c := waitChannel(t, v, address, "connecting")
	target, _ := Target(address)
	stopped := make(chan error, 1)
	go func() { stopped <- v.Stop(target) }()
	// Observe Stop's cancellation before letting config preparation finish.
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		v.mu.Lock()
		cancelled := c.stopped
		v.mu.Unlock()
		if cancelled {
			break
		}
		time.Sleep(time.Millisecond)
	}
	v.configMu.Unlock()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if err := <-resolved; err == nil {
		t.Fatal("cancelled start returned ready")
	}
	advanceRetry(v)
	if _, err := v.Resolve(t.Context(), address); err == nil {
		t.Fatal("delayed start resurrected stopped channel")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.generation != 1 || c.pid != 0 {
		t.Fatal("cancelled preparation launched a process")
	}
}
