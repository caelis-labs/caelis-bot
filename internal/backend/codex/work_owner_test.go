package codex

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRetainedOwnerConnectsOriginalWorkerWithoutResidentResume(t *testing.T) {
	root := t.TempDir()
	opts := SessionOptions{Directory: filepath.Join(root, "Work"), StateFile: filepath.Join(root, "conversation.json")}
	b, _ := json.Marshal(binding{Version: 1, ThreadID: "original-bot", Pending: &pendingSubmission{ID: "resident-request"}, Tasks: map[string]*taskRecord{
		"original-task": {Thread: "original-worker", View: api.Task{ID: "original-task", Workspace: root, Status: "completed"}},
	}})
	if err := os.WriteFile(opts.StateFile, b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWorkOwner(opts); err == nil {
		t.Fatal("fresh remote owner accepted a resident binding")
	}
	w, err := NewRetainedWorkOwner(opts)
	if err != nil {
		t.Fatal(err)
	}
	f := &sessionFixture{answers: make(chan wireMessage, 8)}
	resumed := make(chan string, 8)
	f.handle = func(m wireMessage) (any, bool) {
		switch m.Method {
		case "thread/start", "turn/start":
			t.Error("inactive owner dispatched a new resident thread/turn")
		case "thread/resume":
			var p struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(m.Params, &p)
			resumed <- p.ThreadID
			return map[string]any{"thread": nativeThread{ID: p.ThreadID}}, true
		case "thread/read":
			thread := nativeThread{ID: "original-worker"}
			thread.Status.Type = "notLoaded"
			return map[string]any{"thread": thread}, true
		}
		return nil, false
	}
	w.engine.start = func(context.Context, Options) (*Client, error) {
		a, b := net.Pipe()
		go f.serve(b)
		return &Client{rpc: newTransportOptions(a, nil, true)}, nil
	}
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	if err = w.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-resumed:
		t.Fatal("completed historical Worker was loaded on connect", id)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := w.ReadWork(t.Context(), "original-task"); err != nil {
		t.Fatal(err)
	}
	if got := w.WorkStates(); len(got) != 1 || got[0].Task.ID != "original-task" {
		t.Fatal("retained task lost")
	}
	if w.engine.bound || w.engine.binding.ThreadID != "original-bot" {
		t.Fatal("inactive resident binding changed")
	}
}
