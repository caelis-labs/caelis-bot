package workerpeer

import (
	"fmt"
	"os"
	"runtime/trace"
	"testing"
)

// CI-only trace of the original package invocation, not a second attempt. The
// opt-in path contains only fixture events and does not affect product builds.
func TestMain(m *testing.M) {
	path := os.Getenv("CAELIS_BOT_TEST_WORKERPEER_TRACE")
	if path == "" {
		os.Exit(m.Run())
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "workerpeer diagnostic unavailable:", err)
		os.Exit(m.Run())
	}
	if err = trace.Start(f); err != nil {
		f.Close()
		fmt.Fprintln(os.Stderr, "workerpeer diagnostic unavailable:", err)
		os.Exit(m.Run())
	}
	code := m.Run()
	trace.Stop()
	if err = f.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "workerpeer diagnostic close:", err)
	}
	os.Exit(code)
}
