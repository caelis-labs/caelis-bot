//go:build !windows

package eventplan

import (
	"context"
	"encoding/json"
	"strings"
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
