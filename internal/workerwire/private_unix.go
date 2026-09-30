//go:build darwin || linux

package workerwire

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

func pairFileOwner(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}

func privatePath(path string, existing bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("Worker socket requires private absolute path")
	}
	dir := filepath.Dir(path)
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil || canonical != dir {
		return errors.New("Worker socket directory redirected")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("Worker socket directory must be private")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return errors.New("Worker socket directory owner mismatch")
	}
	if existing {
		info, err = os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
			return errors.New("Worker socket must be private")
		}
		stat, ok = info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Getuid()) {
			return errors.New("Worker socket owner mismatch")
		}
	}
	return nil
}

// ServeUnix closes only wire sessions when its service context ends. The caller
// owns explicit Owner.Stop; neither this listener nor its helpers reap Codex.
func (s *Server) ServeUnix(ctx context.Context, path string) error {
	return s.ServeUnixReady(ctx, path, nil)
}

// ServeUnixReady publishes native readiness only after the private listener is
// bound and chmodded. The callback grants no authority and does not own shutdown.
func (s *Server) ServeUnixReady(ctx context.Context, path string, ready func()) error {
	if err := privatePath(path, false); err != nil {
		return err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return errors.New("Worker socket already owned or unavailable")
	}
	defer listener.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return err
	}
	if ready != nil {
		ready()
	}
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	var clients sync.WaitGroup
	defer clients.Wait()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		clients.Go(func() { _ = s.Serve(ctx, conn) })
	}
}

// ProxyUnix is a one-observer same-user native helper for strict SSH. It uses
// the pre-existing private service socket and cannot start or stop the owner.
func ProxyUnix(ctx context.Context, in io.Reader, out io.Writer, path string) error {
	if err := privatePath(path, true); err != nil {
		return err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return errors.New("Worker owner unavailable")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() {
		_ = conn.Close()
		if closer, ok := in.(io.Closer); ok {
			_ = closer.Close()
		}
	})
	defer stop()
	finished := make(chan struct{}, 1)
	go func() { _, _ = io.Copy(conn, in); _ = conn.Close(); finished <- struct{}{} }()
	_, err = io.Copy(out, conn)
	_ = conn.Close()
	if closer, ok := in.(io.Closer); ok {
		_ = closer.Close()
	}
	<-finished
	return err
}
