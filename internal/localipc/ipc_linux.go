package localipc

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func Listen() (*Listener, error) {
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
		// Closing releases blocked Accept; accepted connections retain their own
		// lifetime so callers can drain them before ending tool dispatch.
		return errors.Join(socket.Close(), os.RemoveAll(dir))
	}}, nil
}

func Dial(endpoint string, timeout time.Duration) (net.Conn, error) {
	if !filepath.IsAbs(endpoint) {
		return nil, errors.New("private IPC endpoint must be absolute")
	}
	for path, socket := range map[string]bool{endpoint: true, filepath.Dir(endpoint): false} {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0077 != 0 || info.Mode()&os.ModeSymlink != 0 || (socket && info.Mode()&os.ModeSocket == 0) || (!socket && !info.IsDir()) {
			return nil, errors.New("IPC endpoint is not private to this user")
		}
	}
	return net.DialTimeout("unix", endpoint, timeout)
}
