package codex

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestExistingServerNeedsNoCLIAndCloseDoesNotStopServer(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("local socket discovery is macOS only")
	}
	// Darwin Unix socket names are short; ordinary test temp paths can exceed it.
	dir, err := os.MkdirTemp("/tmp", "cb-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "server.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	var connected, initialized atomic.Int32
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		connected.Add(1)
		for {
			_, b, err := ws.Read(r.Context())
			if err != nil {
				return
			}
			var m wireMessage
			if json.Unmarshal(b, &m) != nil {
				return
			}
			var result any
			switch m.Method {
			case "initialize":
				result = map[string]string{"userAgent": "synthetic", "codexHome": dir, "platformFamily": "unix", "platformOs": "macos"}
			case "initialized":
				initialized.Add(1)
				continue
			case "account/read":
				result = map[string]any{"requiresOpenaiAuth": true, "account": nil}
			case "thread/backgroundTerminals/list":
				result = map[string]any{"data": []any{}, "nextCursor": nil}
			case "thread/backgroundTerminals/clean":
				result = map[string]any{}
			case "turn/interrupt":
				event, _ := json.Marshal(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "fixture", "turn": nativeTurn{ID: "turn", Status: "interrupted"}})})
				if ws.Write(r.Context(), websocket.MessageText, event) != nil {
					return
				}
				result = map[string]any{}
			default:
				return
			}
			// Pretty JSON exercises frame boundaries instead of assuming JSONL.
			out, _ := json.MarshalIndent(map[string]any{"id": m.ID, "result": result}, "", "  ")
			if ws.Write(r.Context(), websocket.MessageText, out) != nil {
				return
			}
		}
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for range 2 {
		c, err := Start(ctx, Options{Socket: path, Binary: "/does/not/exist"})
		if err != nil {
			t.Fatal(err)
		}
		auth, err := c.ReadAuthStatus(ctx)
		c.Close()
		if err != nil || auth.AccountPresent || !auth.RequiresOpenAIAuth {
			t.Fatal("handshake/account projection failed", err)
		}
	}
	if connected.Load() != 2 || initialized.Load() != 2 {
		t.Fatal("client Close stopped the listener or handshake was skipped")
	}
	// Exercise Session's production interrupt path too. A shared server must
	// retain its connection instead of taking the owned-process recycle path.
	c, err := Start(ctx, Options{Socket: path})
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession(SessionOptions{StateFile: filepath.Join(dir, "binding.json")})
	s.client = c
	s.bound = true
	s.binding.ThreadID = "fixture"
	s.run = "turn"
	s.state.Connection = "ready"
	go s.listen(c, s.epoch)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	if err := s.Interrupt(ctx); err != nil {
		t.Fatal(err)
	}
	if c.Err() != nil || s.client != c || connected.Load() != 3 || s.Snapshot().Phase != "interrupted" {
		t.Fatal("interrupt recycled an existing server connection")
	}
}
