package codex

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/coder/websocket"
)

func TestUnavailableSocketFallsBackToCLI(t *testing.T) {
	for _, mode := range []string{"missing", "bad-handshake"} {
		t.Run(mode, func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "cb-fallback-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			path := filepath.Join(dir, "server.sock")
			if mode == "bad-handshake" {
				listener, err := net.Listen("unix", path)
				if err != nil {
					t.Fatal(err)
				}
				server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					ws, err := websocket.Accept(w, r, nil)
					if err != nil {
						return
					}
					defer ws.CloseNow()
					_, _, _ = ws.Read(r.Context())
					_ = ws.Write(r.Context(), websocket.MessageText, []byte(`{"id":1,"result":{}}`))
				})}
				go func() { _ = server.Serve(listener) }()
				t.Cleanup(func() { _ = server.Close() })
			}
			binary, _ := fixtureBinary(t, "normal")
			c, err := Start(testContext(t), Options{Socket: path, Binary: binary})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if c.UsesSharedServer() {
				t.Fatal("invalid socket prevented CLI fallback")
			}
			if _, err := c.ReadAuthStatus(testContext(t)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCLIPathValidationPrecedesPersistence(t *testing.T) {
	for _, mode := range []string{"bad-init", "unsupported-init", "normal", "older-compatible", "newer-compatible"} {
		t.Run(mode, func(t *testing.T) {
			binary, _ := fixtureBinary(t, mode)
			s := NewSession(SessionOptions{Directory: t.TempDir(), StateFile: filepath.Join(t.TempDir(), "binding.json")})
			defer s.Close(context.Background())
			persisted := false
			_, err := s.ChangeCLI(testContext(t), binary, func() error { persisted = true; return errors.New("fixture store unavailable") })
			compatible := mode == "normal" || mode == "older-compatible" || mode == "newer-compatible"
			if err == nil || persisted != compatible || s.opts.Binary != "" {
				t.Fatal("changed settings before validation/durable save", mode, err)
			}
		})
	}
}
