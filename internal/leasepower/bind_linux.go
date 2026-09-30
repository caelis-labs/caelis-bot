//go:build linux

package leasepower

import (
	"context"
	"syscall"

	"github.com/godbus/dbus/v5"
)

func markCloseOnExec(fd int) { syscall.CloseOnExec(fd) }

type nativeConnection struct {
	conn       *dbus.Conn
	registered bool
}

func (c *nativeConnection) Call(ctx context.Context, dest string, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	call := c.conn.Object(dest, path).CallWithContext(ctx, method, 0, args...)
	return call.Body, call.Err
}
func (c *nativeConnection) Subscribe(ctx context.Context, signals chan<- *dbus.Signal, options ...dbus.MatchOption) error {
	if !c.registered {
		c.conn.Signal(signals)
		c.registered = true
	}
	return c.conn.AddMatchSignalContext(ctx, options...)
}
func (c *nativeConnection) SupportsUnixFDs() bool { return c.conn.SupportsUnixFDs() }
func (c *nativeConnection) Done() <-chan struct{} { return c.conn.Context().Done() }
func (c *nativeConnection) Close() error          { return c.conn.Close() }

func BindWithOptions(ctx context.Context, suspend, wake func(), opts Options) (func(), error) {
	if _, err := checkedOptions(suspend, wake, opts); err != nil {
		if suspend != nil {
			suspend()
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		suspend()
		return nil, unavailable("binding context ended", err)
	}
	// Keep this private connection alive until watcher cleanup has fenced the
	// native owner. Binding lifetime cancellation must not close it first.
	conn, err := dbus.ConnectSystemBus(dbus.WithSignalHandler(dbus.NewSequentialSignalHandler()))
	if err != nil {
		suspend()
		return nil, unavailable("Linux system bus connection failed", err)
	}
	return bindConnection(ctx, &nativeConnection{conn: conn}, suspend, wake, opts)
}
