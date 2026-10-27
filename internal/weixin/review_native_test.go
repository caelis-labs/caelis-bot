//go:build !windows

package weixin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/coder/websocket"
)

func TestReviewNativeCompletionPromotesRetiredUnchangedItem(t *testing.T) {
	native := newCompletionRuntime(t)
	f := newDeliveryFixture(t)
	s := codex.NewSession(codex.SessionOptions{Socket: native.path, RequiredSocket: true, Binary: "/fixture/no-cli", Directory: filepath.Join(f.root, "work"), StateFile: filepath.Join(f.root, "binding.json")})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	service := backend.NewService(s, nil, nil, nil, nil)
	t.Cleanup(func() {
		closeCtx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = s.Close(closeCtx)
		_ = service.Shutdown()
	})
	service.ConfigureChat(filepath.Join(f.root, "chat.sqlite"))
	f.b.host.Snapshot = service.Snapshot
	f.b.state.Window.Replied = true
	f.b.state.Delivery.LastSent = f.now.UnixMilli()
	if _, err := s.Submit(t.Context(), api.Submission{ID: "native-input", Text: "work"}, nil); err != nil {
		t.Fatal(err)
	}
	native.comment(t, "native-input", "The same completed native item")
	working := awaitCompletionSnapshot(t, s, func(v api.Snapshot) bool {
		for _, item := range v.Items {
			if item.Text == "The same completed native item" {
				return v.Phase == "working"
			}
		}
		return false
	})
	service.ObserveChat(working)
	f.poll()
	f.window("B")
	f.poll()
	f.reopen()
	native.mu.Lock()
	native.turn["status"] = "completed"
	data, _ := json.Marshal(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": native.thread, "turn": native.turn}})
	ws := native.ws
	native.mu.Unlock()
	if err := ws.Write(t.Context(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
	terminal := awaitCompletionSnapshot(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" })
	service.ObserveChat(terminal)
	f.poll()
	if len(f.sent()) != 1 || !strings.Contains(f.sent()[0], "The same completed native item") {
		t.Fatal("native unchanged final was lost across new window/restart", f.sent())
	}
}
