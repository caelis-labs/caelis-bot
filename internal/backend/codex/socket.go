package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/coder/websocket"
)

var errExistingServer = errors.New("existing app server unavailable")

// Prefer an already listening standard control socket. This does not launch an
// app or daemon, inspect private app IPC, or require a CLI binary to connect.
func connectExisting(ctx context.Context, path string) (connection, error) {
	if runtime.GOOS == "darwin" {
		if path == "" {
			home := os.Getenv("CODEX_HOME")
			if home == "" {
				user, _ := os.UserHomeDir()
				home = filepath.Join(user, ".codex")
			}
			path = filepath.Join(home, "app-server-control", "app-server-control.sock")
		}
		info, err := os.Stat(path)
		if err == nil {
			if err != nil || !filepath.IsAbs(path) || info.Mode()&os.ModeSocket == 0 {
				return nil, errExistingServer
			}
			conn, err := dialExisting(ctx, path)
			if err != nil {
				return nil, errExistingServer
			}
			return conn, nil // Closing this client must never stop the shared server.
		}
	}
	return nil, errExistingServer
}

type socketConnection struct {
	endpoint string
	net.Conn
	ws     *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	buffer []byte
}

func dialExisting(ctx context.Context, path string) (*socketConnection, error) {
	dialer := &net.Dialer{}
	httpTransport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dialer.DialContext(ctx, "unix", path) }}
	ws, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: &http.Client{Transport: httpTransport}})
	defer httpTransport.CloseIdleConnections()
	if err != nil {
		return nil, err
	}
	life, cancel := context.WithCancel(context.Background())
	conn := websocket.NetConn(life, ws, websocket.MessageText)
	ws.SetReadLimit(8 * 1024 * 1024)
	return &socketConnection{Conn: conn, ws: ws, ctx: life, cancel: cancel, endpoint: "unix://" + path}, nil
}

func (s *socketConnection) terminalEndpoint() string { return s.endpoint }

// The shared transport carries one JSON object per text frame, not JSONL. Add a
// delimiter only at this adapter boundary so the existing correlated RPC reader
// retains its limits and server-request sequencing.
func (s *socketConnection) Read(b []byte) (int, error) {
	if len(s.buffer) == 0 {
		kind, data, err := s.ws.Read(s.ctx)
		if err != nil {
			return 0, err
		}
		if kind != websocket.MessageText {
			return 0, ErrProtocol
		}
		var compact bytes.Buffer
		if json.Compact(&compact, data) != nil {
			return 0, ErrProtocol
		}
		s.buffer = append(compact.Bytes(), '\n')
	}
	n := copy(b, s.buffer)
	s.buffer = s.buffer[n:]
	return n, nil
}
func (s *socketConnection) Close() error {
	s.cancel()
	_ = s.ws.CloseNow()
	return s.Conn.Close()
}
