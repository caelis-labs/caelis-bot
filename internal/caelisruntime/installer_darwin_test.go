package caelisruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/i18n"
)

func TestCancelledInstallerStopsDownloadChild(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "child-survived")
	t.Setenv("INSTALL_CHILD_MARKER", marker)
	script := filepath.Join(root, "installer.sh")
	if err := os.WriteFile(script, []byte("(/bin/sleep 1; /bin/echo survived > \"$INSTALL_CHILD_MARKER\") &\nwait\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := runInstaller(ctx, i18n.English, script, "file:///tmp/empty"); err == nil {
		t.Fatal("cancelled installer reported success")
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("installer child survived cancellation: %v", err)
	}
}
