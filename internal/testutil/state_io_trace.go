package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/trace"
	"testing"
	"time"
)

// RunStateIOTrace records only opt-in synthetic test processes. A snapshot before
// the CI package's existing 180s deadline survives testing's fatal timeout. The
// final snapshot captures ordinary success/failure. Neither reruns the tests.
// Tracing adds overhead; timings diagnose this invocation, not a prior run.
func RunStateIOTrace(m *testing.M, scope string) int {
	dir := os.Getenv("CAELIS_BOT_TEST_STATE_IO_TRACE")
	if dir == "" {
		return m.Run()
	}
	recorder := trace.NewFlightRecorder(trace.FlightRecorderConfig{MinAge: 180 * time.Second, MaxBytes: 32 << 20})
	if err := recorder.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "state I/O trace unavailable:", err)
		return m.Run()
	}
	snapshot := func(phase string) {
		name := fmt.Sprintf("%s-%d-%s.trace", scope, os.Getpid(), phase)
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "state I/O trace open:", err)
			return
		}
		_, err = recorder.WriteTo(f)
		closed := f.Close()
		if err != nil || closed != nil {
			fmt.Fprintln(os.Stderr, "state I/O trace snapshot:", err, closed)
		}
	}
	snapshotDone := make(chan struct{})
	timer := time.AfterFunc(165*time.Second, func() { defer close(snapshotDone); snapshot("predeadline") })
	code := m.Run()
	if !timer.Stop() {
		<-snapshotDone
	}
	snapshot("final")
	recorder.Stop()
	return code
}
