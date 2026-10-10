//go:build darwin

package codex

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/coder/websocket"
)

// The resident path must check trust even when Start attaches to an existing
// shared App Server instead of launching the private CLI process.
func TestSharedAppServerChecksBotWorkspaceTrust(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "cb-trust-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	workspace := filepath.Join(root, "Notebook")
	socket := filepath.Join(root, "shared.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var mu sync.Mutex
	trusted, writes, starts := false, 0, 0
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		for {
			_, frame, err := ws.Read(r.Context())
			if err != nil {
				return
			}
			var request wireMessage
			if json.Unmarshal(frame, &request) != nil || request.Method == "" || len(request.ID) == 0 {
				continue
			}
			mu.Lock()
			var result any = map[string]any{}
			switch request.Method {
			case "initialize":
				result = map[string]string{"userAgent": "shared-fixture"}
			case "account/read":
				result = map[string]any{"account": map[string]string{"type": "apiKey"}, "requiresOpenaiAuth": true}
			case "config/read":
				projects := map[string]any{}
				reason := "untrusted"
				if trusted {
					projects[workspace] = map[string]string{"trust_level": "trusted"}
					reason = ""
				}
				result = map[string]any{"config": map[string]any{"projects": projects}, "layers": []any{map[string]any{"name": map[string]string{"type": "project"}, "disabledReason": reason}}}
			case "config/batchWrite":
				writes++
				trusted = true
			case "thread/start":
				starts++
				result = map[string]any{"thread": nativeThread{ID: "shared-resident"}, "model": "fixture"}
			}
			mu.Unlock()
			response, _ := json.Marshal(wireMessage{ID: request.ID, Result: raw(result)})
			if ws.Write(r.Context(), websocket.MessageText, response) != nil {
				return
			}
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	s := NewSession(SessionOptions{Socket: socket, RequiredSocket: true, Directory: workspace, StateFile: filepath.Join(root, "binding.json")})
	if err := s.ConfigureBotTools(&api.ToolConnection{Command: "/fixture/bot", NotebookDirectory: workspace}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if err := s.Connect(ctx); err != nil {
		t.Fatal(err, s.Snapshot())
	}
	defer s.Close(context.Background())
	if !s.client.UsesSharedServer() || s.Snapshot().Connection != "ready" {
		t.Fatal("shared owner did not become ready", s.Snapshot())
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != 1 || starts != 1 {
		t.Fatal("shared trust was not persisted before resident initialization", writes, starts)
	}
}
