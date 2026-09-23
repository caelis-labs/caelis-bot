package localipc

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"time"
)

func Listen() (*Listener, error) {
	// A short private directory avoids Darwin's Unix-socket path limit; the
	// user's macOS TMPDIR can already consume most of that limit.
	dir, err := os.MkdirTemp("/tmp", "caelis-bot-")
	if err != nil {
		return nil, err
	}
	endpoint := filepath.Join(dir, "tools.sock")
	socket, err := net.Listen("unix", endpoint)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if err = os.Chmod(endpoint, 0600); err != nil {
		_ = socket.Close()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &Listener{Listener: socket, endpoint: endpoint, cleanup: func() error {
		return errors.Join(socket.Close(), os.RemoveAll(dir))
	}}, nil
}
func Dial(endpoint string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", endpoint, timeout)
}
