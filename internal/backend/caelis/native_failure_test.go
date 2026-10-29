package caelis

import (
	"os"
	"path/filepath"
	"testing"
)

// Only the isolated synthetic Host test calls this helper. Production error
// strings stay redacted; fixture diagnostics must survive TempDir cleanup.
func preserveNativeHostFailure(t *testing.T, root string) {
	t.Helper()
	if !t.Failed() {
		return
	}
	if data, err := os.ReadFile(filepath.Join(root, "host.log")); err == nil {
		t.Logf("synthetic Host log:\n%s", data)
	}
	out := os.Getenv("CAELIS_BOT_TEST_EVIDENCE_DIR")
	if out == "" {
		return
	}
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Errorf("create native failure evidence directory: %v", err)
		return
	}
	// Keep the stopped Store, original Bot journal and logs together without
	// following fixture symlinks. Use a directory on the same filesystem.
	target := filepath.Join(out, filepath.Base(filepath.Dir(root)))
	if err := os.Rename(root, target); err != nil {
		t.Errorf("preserve native failure evidence: %v", err)
		return
	}
	t.Logf("native failure evidence: %s", target)
}
