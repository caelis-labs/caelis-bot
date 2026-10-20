package weixin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdatePreservesPausedAndEnabledUnknown(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		root := t.TempDir()
		b, _ := Open(root, Host{})
		b.state.Enabled = enabled
		b.state.Cursor = "original-cursor"
		b.state.Inputs["original"] = "unknown"
		b.state.Outputs["out-original"] = outbound{State: "unknown"}
		if err := b.PrepareUpdate(); err != nil {
			t.Fatal(err)
		}
		if err := b.CloseForUpdate(); err != nil {
			t.Fatal(err)
		}
		next, err := Open(root, Host{})
		if err != nil || next.state.Enabled != enabled || next.state.Cursor != "original-cursor" || next.state.Inputs["original"] != "unknown" || next.state.Outputs["out-original"].State != "unknown" {
			t.Fatal("handoff changed original receipts", err)
		}
	}
}

func TestUpdateWaitsForOwnedFileCompletion(t *testing.T) {
	b, _ := Open(t.TempDir(), Host{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	b.cancel, b.done = cancel, done
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ret":0}`)) }))
	defer server.Close()
	b.client = &protocol{base: server.URL, client: server.Client()}
	b.fileWait.Add(1)
	finished := make(chan struct{})
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		b.state.Outputs["file-original"] = outbound{State: "unknown"}
		b.mu.Unlock()
		b.fileWait.Done()
		close(finished)
		close(done)
	}()
	if err := b.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("file work escaped handoff")
	}
	if b.state.Outputs["file-original"].State != "unknown" {
		t.Fatal("unknown file effect erased")
	}
}
