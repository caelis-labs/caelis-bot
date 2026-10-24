//go:build workerpeere2e

// This binary replaces only the synthetic Codex protocol in Worker peer E2E
// tests. It is excluded from ordinary and release builds.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/coder/websocket"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "worker peer native fixture:", err)
		os.Exit(1)
	}
}

func run(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 1 && args[0] == "--version" {
		_, err := fmt.Fprintln(out, "codex-worker-peer-fixture 0.1.0 (synthetic; workerpeere2e)")
		return err
	}
	if len(args) != 3 || args[0] != "app-server" || args[1] != "--listen" {
		return errors.New("expected app-server --listen unix://PATH or stdio://")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root := os.Getenv("WORKER_PEER_FIXTURE_ROOT")
	rel, relErr := filepath.Rel(root, cwd)
	if os.Getenv("WORKER_PEER_NATIVE_FIXTURE") != "1" || !filepath.IsAbs(root) || relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("explicit isolated WORKER_PEER_NATIVE_FIXTURE=1 and WORKER_PEER_FIXTURE_ROOT are required")
	}
	s, err := openNative(cwd)
	if err != nil {
		return err
	}
	defer s.close()
	if args[2] == "stdio://" {
		return s.serve(in, out)
	}
	if !strings.HasPrefix(args[2], "unix://") {
		return errors.New("fixture supports only private unix:// and stdio://")
	}
	path := strings.TrimPrefix(args[2], "unix://")
	if !filepath.IsAbs(path) {
		return errors.New("absolute private socket required")
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer ln.Close()
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() { <-ctx.Done(); _ = ln.Close() }()
	err = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		conn := websocket.NetConn(context.Background(), ws, websocket.MessageText)
		defer conn.Close()
		_ = s.serve(conn, conn)
	}))
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func copyJSON[T any](value T) T {
	data, _ := json.Marshal(value)
	var copy T
	_ = json.Unmarshal(data, &copy)
	return copy
}
