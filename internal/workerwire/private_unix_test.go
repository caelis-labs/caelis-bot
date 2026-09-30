//go:build darwin || linux

package workerwire

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkerListenerReadinessFollowsPrivateBindAndPrecedesAccept(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "worker-ready-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "worker.sock")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	// No owner methods are needed to bind and detach an observer listener.
	server := &Server{}
	err = server.ServeUnixReady(ctx, socket, func() {
		calls++
		if err := privatePath(socket, true); err != nil {
			t.Error("readiness preceded private socket publication", err)
		}
		conn, err := net.Dial("unix", socket)
		if err != nil {
			t.Error("readiness announced before listener accepted connections", err)
		} else {
			_ = conn.Close()
		}
		cancel()
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("listener did not detach after native readiness", calls, err)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatal("detached listener left its socket", err)
	}
	// Never publish readiness for an unsafe directory or overwrite an occupied path.
	if err = os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err = server.ServeUnixReady(t.Context(), socket, func() { calls++ }); err == nil || calls != 1 {
		t.Fatal("unsafe listener advertised readiness", calls, err)
	}
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(socket, []byte("existing private data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = server.ServeUnixReady(t.Context(), socket, func() { calls++ }); err == nil || calls != 1 {
		t.Fatal("occupied listener advertised readiness", calls, err)
	}
	bytes, err := os.ReadFile(socket)
	if err != nil || string(bytes) != "existing private data" {
		t.Fatal("listener overwrote occupied path", err)
	}
}
