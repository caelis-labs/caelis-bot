package nodebroker

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodecoord"
)

type streamAddress string

func (a streamAddress) Network() string { return "ssh-private-broker" }
func (a streamAddress) String() string  { return string(a) }

// HTTP request cancellation closes this owned helper stream. HTTP client's
// bounded timeout is authoritative; SSH has its own strict connection timeout.
type streamConnection struct{ io.ReadWriteCloser }

func (c *streamConnection) LocalAddr() net.Addr              { return streamAddress("local") }
func (c *streamConnection) RemoteAddr() net.Addr             { return streamAddress("paired") }
func (c *streamConnection) SetDeadline(time.Time) error      { return nil }
func (c *streamConnection) SetReadDeadline(time.Time) error  { return nil }
func (c *streamConnection) SetWriteDeadline(time.Time) error { return nil }

// NewSSHClient uses only the existing strict SSH helper transport, with a fixed
// proxy-broker operation and native pinned broker enrollment. No credential or
// network configuration is copied, generated or persisted.
func NewSSHClient(ctx context.Context, ssh nodeagent.SSHConfig, helper, socket, expectedBroker string) (*Client, error) {
	if expectedBroker == "" {
		return nil, nodecoord.ErrIneligible
	}
	t := &http.Transport{DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
		if err := dialCtx.Err(); err != nil {
			return nil, err
		}
		stream, err := nodeagent.OpenBrokerStream(ctx, ssh, helper, socket)
		if err != nil {
			return nil, err
		}
		if err = dialCtx.Err(); err != nil {
			stream.Close()
			return nil, err
		}
		return &streamConnection{stream}, nil
	}, MaxConnsPerHost: 4, MaxIdleConns: 4, ResponseHeaderTimeout: 10 * time.Second}
	c := &Client{http: &http.Client{Transport: t, Timeout: 15 * time.Second}, transport: t, expectedBroker: expectedBroker}
	out, err := c.call(ctx, "/identity", envelope{})
	if err != nil || out.BrokerNodeID != expectedBroker {
		c.Close()
		return nil, nodecoord.ErrIneligible
	}
	return c, nil
}

// ProxyUnix relays only a pre-existing same-user private broker connection.
// EOF closes this observer helper; it never starts or stops the broker owner.
func ProxyUnix(ctx context.Context, in io.Reader, out io.Writer, socket string) error {
	if err := privatePath(socket, true); err != nil {
		return err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return errors.New("paired private broker unavailable")
	}
	defer conn.Close()
	cancel := context.AfterFunc(ctx, func() {
		_ = conn.Close()
		if closer, ok := in.(io.Closer); ok {
			_ = closer.Close()
		}
	})
	defer cancel()
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(conn, in)
		if unix, ok := conn.(*net.UnixConn); ok {
			_ = unix.CloseWrite()
		}
		close(copied)
	}()
	_, err = io.Copy(out, conn)
	_ = conn.Close()
	if closer, ok := in.(io.Closer); ok {
		_ = closer.Close()
	}
	<-copied
	return err
}
