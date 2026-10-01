// Package nodebroker carries the closed coordinator RPC over same-user private
// Unix IPC. Existing SSH can reverse-forward the socket; no listener, credentials
// or network configuration is installed by this package.
package nodebroker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/nodecoord"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

type Port interface {
	nodeplane.Coordinator
	nodeplane.SnapshotPublisher
	nodeplane.SnapshotReader
	SnapshotState(context.Context) (nodeplane.Lease, nodeplane.SnapshotRef, error)
}

type envelope struct {
	Claim    *nodeplane.ClaimRequest `json:"claim,omitempty"`
	Lease    *nodeplane.Lease        `json:"lease,omitempty"`
	Snapshot *nodeplane.SnapshotRef  `json:"snapshot,omitempty"`
	BotID    string                  `json:"botId,omitempty"`
	Payload  []byte                  `json:"payload,omitempty"`
}
type response struct {
	Lease    nodeplane.Lease       `json:"lease"`
	Snapshot nodeplane.SnapshotRef `json:"snapshot"`
	Payload  []byte                `json:"payload,omitempty"`
	Code     string                `json:"code,omitempty"`
}

const maxFrame = 24 << 20

func code(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, nodecoord.ErrConflict):
		return "conflict"
	case errors.Is(err, nodecoord.ErrSnapshot):
		return "snapshot_required"
	case errors.Is(err, nodecoord.ErrIneligible):
		return "ineligible"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	default:
		return "unavailable"
	}
}
func fromCode(c string) error {
	switch c {
	case "":
		return nil
	case "conflict":
		return nodecoord.ErrConflict
	case "snapshot_required":
		return nodecoord.ErrSnapshot
	case "ineligible":
		return nodecoord.ErrIneligible
	default:
		return nodecoord.ErrUnavailable
	}
}

// Handler admits only explicitly typed operations. Native identities, paths,
// executable actions and credentials have no generic dispatch surface.
func Handler(p Port) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxFrame))
		dec.DisallowUnknownFields()
		var in envelope
		if dec.Decode(&in) != nil || dec.Decode(&struct{}{}) != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var out response
		var err error
		switch r.URL.Path {
		case "/claim":
			if in.Claim == nil {
				err = nodecoord.ErrIneligible
			} else {
				out.Lease, err = p.Claim(r.Context(), *in.Claim)
			}
		case "/heartbeat":
			if in.Lease == nil {
				err = nodecoord.ErrConflict
			} else {
				out.Lease, err = p.Heartbeat(r.Context(), *in.Lease)
			}
		case "/release":
			if in.Lease == nil {
				err = nodecoord.ErrConflict
			} else {
				err = p.Release(r.Context(), *in.Lease)
			}
		case "/publish":
			if in.Lease == nil || in.Snapshot == nil {
				err = nodecoord.ErrSnapshot
			} else {
				err = p.PublishSnapshot(r.Context(), *in.Lease, *in.Snapshot, in.Payload)
			}
		case "/latest":
			out.Snapshot, err = p.LatestSnapshot(r.Context(), in.BotID)
		case "/snapshot":
			if in.Snapshot == nil {
				err = nodecoord.ErrSnapshot
			} else {
				out.Payload, err = p.ReadSnapshot(r.Context(), *in.Snapshot)
			}
		case "/state":
			out.Lease, out.Snapshot, err = p.SnapshotState(r.Context())
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		out.Code = code(err)
		_ = json.NewEncoder(w).Encode(out)
	})
}

func privatePath(path string, existing bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("broker socket requires absolute private path")
	}
	dir := filepath.Dir(path)
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil || canonical != dir {
		return errors.New("broker socket directory redirected")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !fileOwner(info) {
		return errors.New("broker socket directory must be private and owned")
	}
	if existing {
		info, err = os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 || !fileOwner(info) {
			return errors.New("broker socket must be private and owned")
		}
	}
	return nil
}
func ServeUnix(ctx context.Context, path string, p Port, ready func()) error {
	if err := privatePath(path, false); err != nil {
		return err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer l.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return err
	}
	server := &http.Server{Handler: Handler(p), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 4096, BaseContext: func(net.Listener) context.Context { return ctx }}
	stop := context.AfterFunc(ctx, func() { _ = server.Close() })
	defer stop()
	if ready != nil {
		ready()
	}
	err = server.Serve(l)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

type Client struct {
	http      *http.Client
	transport *http.Transport
	mu        sync.Mutex
	closed    bool
}

func DialUnix(path string) (*Client, error) {
	if err := privatePath(path, true); err != nil {
		return nil, err
	}
	t := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		if err := privatePath(path, true); err != nil {
			return nil, err
		}
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}, MaxConnsPerHost: 4, MaxIdleConns: 4, ResponseHeaderTimeout: 10 * time.Second}
	return &Client{http: &http.Client{Transport: t, Timeout: 15 * time.Second}, transport: t}, nil
}
func (c *Client) Close() {
	c.mu.Lock()
	c.closed = true
	c.transport.CloseIdleConnections()
	c.mu.Unlock()
}
func (c *Client) call(ctx context.Context, path string, in envelope) (response, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return response{}, nodecoord.ErrUnavailable
	}
	b, err := json.Marshal(in)
	if err != nil || len(b) > maxFrame {
		return response{}, nodecoord.ErrSnapshot
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://broker"+path, bytes.NewReader(b))
	if err != nil {
		return response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	r, err := c.http.Do(req)
	if err != nil {
		return response{}, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return response{}, nodecoord.ErrUnavailable
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxFrame+1))
	dec.DisallowUnknownFields()
	var out response
	if dec.Decode(&out) != nil || dec.Decode(&struct{}{}) != io.EOF {
		return response{}, nodecoord.ErrUnavailable
	}
	return out, fromCode(out.Code)
}
func (c *Client) Claim(ctx context.Context, r nodeplane.ClaimRequest) (nodeplane.Lease, error) {
	out, err := c.call(ctx, "/claim", envelope{Claim: &r})
	return out.Lease, err
}
func (c *Client) Heartbeat(ctx context.Context, l nodeplane.Lease) (nodeplane.Lease, error) {
	out, err := c.call(ctx, "/heartbeat", envelope{Lease: &l})
	return out.Lease, err
}
func (c *Client) Release(ctx context.Context, l nodeplane.Lease) error {
	_, err := c.call(ctx, "/release", envelope{Lease: &l})
	return err
}
func (c *Client) PublishSnapshot(ctx context.Context, l nodeplane.Lease, s nodeplane.SnapshotRef, b []byte) error {
	_, err := c.call(ctx, "/publish", envelope{Lease: &l, Snapshot: &s, Payload: b})
	return err
}
func (c *Client) LatestSnapshot(ctx context.Context, bot string) (nodeplane.SnapshotRef, error) {
	out, err := c.call(ctx, "/latest", envelope{BotID: bot})
	return out.Snapshot, err
}
func (c *Client) ReadSnapshot(ctx context.Context, s nodeplane.SnapshotRef) ([]byte, error) {
	out, err := c.call(ctx, "/snapshot", envelope{Snapshot: &s})
	return out.Payload, err
}
func (c *Client) SnapshotState(ctx context.Context) (nodeplane.Lease, nodeplane.SnapshotRef, error) {
	out, err := c.call(ctx, "/state", envelope{})
	return out.Lease, out.Snapshot, err
}

// CommitInstall installs only an offline absent generation. Reads before and
// after the local atomic rename reject publication races. A successful return
// is still not execution authority: the native owner must attest this exact
// installed reference and perform Claim's final latest/epoch CAS before adopt
// or start. An error may leave an unused cold generation; never adopt it.
func (c *Client) CommitInstall(ctx context.Context, ref nodeplane.SnapshotRef, install func() error) error {
	if install == nil {
		return nodecoord.ErrSnapshot
	}
	current, err := c.LatestSnapshot(ctx, ref.BotID)
	if err != nil {
		return err
	}
	if current != ref {
		return nodecoord.ErrSnapshot
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = install(); err != nil {
		return err
	}
	current, err = c.LatestSnapshot(ctx, ref.BotID)
	if err != nil {
		return err
	}
	if current != ref {
		return nodecoord.ErrSnapshot
	}
	return ctx.Err()
}
