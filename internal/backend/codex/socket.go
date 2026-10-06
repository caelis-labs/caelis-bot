package codex

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/coder/websocket"
)

var errExistingServer = errors.New("existing app server unavailable")

// Prefer an already listening standard control socket. This does not launch an
// app or daemon, inspect private app IPC, or require a CLI binary to connect.
func connectExisting(ctx context.Context, path string) (connection, error) {
	if runtime.GOOS == "darwin" || (runtime.GOOS == "linux" && path != "") {
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
				return nil, errors.Join(errExistingServer, err)
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
	frame  io.Reader
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
	// Native tool payload size does not limit Bot availability. Stream frames;
	// the wire projector discards tool bytes before allocating an envelope.
	ws.SetReadLimit(-1)
	return &socketConnection{Conn: conn, ws: ws, ctx: life, cancel: cancel, endpoint: "unix://" + path}, nil
}

func (s *socketConnection) terminalEndpoint() string { return s.endpoint }

// The shared transport carries one JSON object per text frame, not JSONL. Add a
// delimiter only at this adapter boundary so the existing correlated RPC reader
// retains its limits and server-request sequencing.
func (s *socketConnection) Read(b []byte) (int, error) {
	if s.frame == nil {
		kind, data, err := s.ws.Reader(s.ctx)
		if err != nil {
			switch {
			case errors.Is(err, websocket.ErrMessageTooBig):
				return 0, ErrFrameTooLarge
			case errors.Is(err, syscall.ECONNRESET):
				return 0, ErrWebSocketReset
			case websocket.CloseStatus(err) != -1:
				return 0, ErrWebSocketClose
			default:
				return 0, classifyReadError(err)
			}
		}
		if kind != websocket.MessageText {
			return 0, ErrProtocol
		}
		s.frame = data
	}
	n, err := s.frame.Read(b)
	if errors.Is(err, io.EOF) {
		s.frame = nil
		if n == 0 {
			b[0] = '\n'
			return 1, nil
		}
		return n, nil
	}
	return n, err
}
func (s *socketConnection) framedJSON() {}
func (s *socketConnection) Close() error {
	s.cancel()
	_ = s.ws.CloseNow()
	return s.Conn.Close()
}
