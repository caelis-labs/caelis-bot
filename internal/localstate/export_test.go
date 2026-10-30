package localstate

// HoldWriterForTest blocks admission at the same per-document gate as Write.
// It exists only in the test binary; no filesystem delay or product hook.
func HoldWriterForTest(path string) func() {
	entry, _ := writers.LoadOrStore(path, make(chan struct{}, 1))
	gate := entry.(chan struct{})
	gate <- struct{}{}
	return func() { <-gate }
}
