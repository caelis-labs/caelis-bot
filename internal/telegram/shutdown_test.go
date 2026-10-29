package telegram

import (
	"sync"
	"testing"
	"testing/synctest"
)

func TestCloseWaitsForCancelledUploadToExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, f := testBridge(t, Host{})
		// Keep the lifecycle proof independent of OS storage latency. The
		// controls/upload integration test separately reopens durable receipts.
		b.path = ""
		paired(b)
		b.state.PendingFiles["upload"] = "fixture/upload.txt"
		blocked := &blockedDocumentClient{fakeClient: f, started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
		releaseUpload := sync.OnceFunc(func() { close(blocked.release) })
		t.Cleanup(releaseUpload)
		b.launch(blocked)
		<-blocked.started

		done := make(chan struct{})
		go func() { b.Close(); close(done) }()
		<-blocked.cancelled
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("Close returned before the cancelled upload exited")
		default:
		}
		releaseUpload()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("Close did not join the exited workers")
		}
		select {
		case <-blocked.finished:
		default:
			t.Fatal("upload is still running after Close")
		}
	})
}
