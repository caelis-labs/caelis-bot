//go:build !windows

package sshtransport

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestFIFOConfigCannotBlockPreviewConnectionOrClose(t *testing.T) {
	v := fixtureTransport(t)
	if e := syscall.Mkfifo(v.path(), 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, e := v.Configuration(); e == nil {
			t.Error("FIFO accepted")
		}
		v.Start(api.SSHTarget{Host: "fixture"})
		v.Close()
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("FIFO blocked transport lifecycle")
	}
}
func TestSlowSSHDoesNotHoldOtherConnectionOrCancellation(t *testing.T) {
	v := fixtureTransport(t)
	script := filepath.Join(t.TempDir(), "slow-ssh")
	put(t, script, "#!/bin/sh\nsleep 60\n")
	os.Chmod(script, 0700)
	v.binary = script
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _, _ = v.Resolve(ctx, "ssh://slow:9317") }()
	time.Sleep(30 * time.Millisecond)
	start := time.Now()
	if a, e := v.Resolve(t.Context(), "http://127.0.0.1:9317"); e != nil || a == "" {
		t.Fatal(a, e)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("read cancellation blocked")
	}
	v.Close()
	if time.Since(start) > time.Second {
		t.Fatal("slow remote blocks other transport/close")
	}
}
