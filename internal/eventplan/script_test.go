//go:build !windows

package eventplan

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestScriptPinnedBytesBoundedOutputAndCancellation(t *testing.T) {
	v, err := PinScript(Event{Interpreter: "/bin/sh", Script: "printf '{\"count\":23}'"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := RunScript(t.Context(), v, t.TempDir())
	if err != nil || string(out) != `{"count":23}` {
		t.Fatal(string(out), err)
	}
	v.ExecutableDigest = "changed"
	if _, err := RunScript(t.Context(), v, t.TempDir()); err == nil {
		t.Fatal("changed binary executed")
	}
	v, _ = PinScript(Event{Interpreter: "/bin/sh", Script: "sleep 30 & wait"})
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := RunScript(ctx, v, t.TempDir()); err == nil || time.Since(start) > time.Second {
		t.Fatal("unbounded collector", err)
	}
	v, _ = PinScript(Event{Interpreter: "/bin/sh", Script: "printf '%s' '" + strings.Repeat("x", 17000) + "'"})
	out, err = RunScript(t.Context(), v, t.TempDir())
	if err == nil || len(out) > 16384 {
		t.Fatal("output exceeded bound")
	}
	_ = json.Valid(out)
}

func TestInterpreterRejectsSpecialOversizeAndCancelledFiles(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0700); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(dir, "large")
	f, err := os.OpenFile(large, os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(maxInterpreterBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	for _, path := range []string{fifo, "/dev/zero", large, dir} {
		start := time.Now()
		if _, err := PinScript(Event{Interpreter: path}); err == nil {
			t.Fatal("accepted", path)
		}
		if time.Since(start) > time.Second {
			t.Fatal("blocked", path)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := PinScriptContext(ctx, Event{Interpreter: "/bin/sh"}); err == nil {
		t.Fatal("ignored cancellation")
	}
}
