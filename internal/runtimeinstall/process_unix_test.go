//go:build darwin || linux

package runtimeinstall

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexInstallerCancellationStopsDescendants(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "deadline"}[deadline], func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "survived")
			ready := filepath.Join(root, "ready")
			t.Setenv("INSTALL_TEST_MARKER", marker)
			t.Setenv("INSTALL_TEST_READY", ready)
			script := filepath.Join(root, "installer.sh")
			if err := os.WriteFile(script, []byte("(/bin/echo ready > \"$INSTALL_TEST_READY\"; /bin/sleep 1; /bin/echo survived > \"$INSTALL_TEST_MARKER\") &\nwait\n"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(t.Context(), 300*time.Millisecond)
			}
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- runCodexInstaller(ctx, script) }()
			until := time.Now().Add(2 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(until) {
					t.Fatal("child did not start")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !deadline {
				cancel()
			}
			select {
			case err := <-done:
				expected := "取消"
				if deadline {
					expected = "超时"
				}
				if err == nil || !strings.Contains(err.Error(), expected) {
					t.Fatal(err)
				}
			case <-time.After(700 * time.Millisecond):
				t.Fatal("installer failed to return promptly")
			}
			time.Sleep(1100 * time.Millisecond)
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("child survived", err)
			}
		})
	}
}
