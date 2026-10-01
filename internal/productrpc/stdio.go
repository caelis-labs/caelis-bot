package productrpc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const maxProxyFrame = 32 << 20

type StdioOptions struct{ ExpectedNode, ExpectedBot string }

type proxyFrame struct {
	ID      string      `json:"id"`
	Method  string      `json:"method,omitempty"`
	Path    string      `json:"path,omitempty"`
	Status  int         `json:"status,omitempty"`
	Headers http.Header `json:"headers,omitempty"`
	Body    []byte      `json:"body,omitempty"`
}

type stdioTransport struct {
	stream    io.ReadWriteCloser
	mu        sync.Mutex
	writeGate chan struct{}
	pending   map[string]chan proxyFrame
	closed    chan struct{}
	once      sync.Once
	next      atomic.Uint64
	validate  func(proxyFrame) bool
}

// NewStdioClient owns only an authenticated SSH proxy stream. Product auth stays
// target-local; no model/Core Host credentials enter this constructor or wire.
func NewStdioClient(opts StdioOptions, stream io.ReadWriteCloser) (*Client, error) {
	if stream == nil || !identifier.MatchString(opts.ExpectedNode) || !identifier.MatchString(opts.ExpectedBot) {
		return nil, errors.New("invalid native product stream pairing")
	}
	t := &stdioTransport{stream: stream, pending: make(map[string]chan proxyFrame), closed: make(chan struct{}), writeGate: make(chan struct{}, 1)}
	t.writeGate <- struct{}{}
	c, err := NewClient(ClientOptions{URL: "http://127.0.0.1:1", ExpectedNode: opts.ExpectedNode, ExpectedBot: opts.ExpectedBot, Token: strings.Repeat("native-proxy-", 3), HTTP: &http.Client{Transport: t}})
	if err != nil {
		return nil, err
	}
	go t.read()
	return c, nil
}

func writeFrame(w io.Writer, f proxyFrame) error {
	b, err := json.Marshal(f)
	if err != nil || len(b) > maxProxyFrame {
		return errors.New("product proxy frame limit")
	}
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(b)))
	for _, part := range [][]byte{prefix[:], b} {
		for len(part) > 0 {
			n, err := w.Write(part)
			if err != nil {
				return err
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			part = part[n:]
		}
	}
	return nil
}

func readFrame(r io.Reader) (proxyFrame, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return proxyFrame{}, err
	}
	n := binary.BigEndian.Uint32(prefix[:])
	if n == 0 || n > maxProxyFrame {
		return proxyFrame{}, errors.New("product proxy frame limit")
	}
	b := make([]byte, int(n))
	if _, err := io.ReadFull(r, b); err != nil {
		return proxyFrame{}, err
	}
	var f proxyFrame
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&f) != nil || d.Decode(new(any)) != io.EOF || !identifier.MatchString(f.ID) {
		return proxyFrame{}, errors.New("invalid product proxy frame")
	}
	return f, nil
}

func (t *stdioTransport) CloseIdleConnections() {
	t.once.Do(func() { close(t.closed); _ = t.stream.Close() })
}

func (t *stdioTransport) read() {
	defer t.CloseIdleConnections()
	for {
		f, err := readFrame(t.stream)
		if err != nil {
			return
		}
		t.mu.Lock()
		reply := t.pending[f.ID]
		t.mu.Unlock()
		if reply != nil {
			select {
			case reply <- f:
			case <-t.closed:
				return
			}
		}
	}
}

func (t *stdioTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body := []byte(nil)
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, MaxResourceBytes+1))
		if err != nil {
			return nil, err
		}
	}
	f := proxyFrame{ID: strconv.FormatUint(t.next.Add(1), 10), Method: r.Method, Path: r.URL.RequestURI(), Headers: proxyHeaders(r.Header), Body: body}
	valid := validProxyRequest(f)
	if t.validate != nil {
		valid = t.validate(f)
	}
	if !valid {
		return nil, errors.New("invalid product proxy request")
	}
	reply := make(chan proxyFrame, 1)
	t.mu.Lock()
	t.pending[f.ID] = reply
	t.mu.Unlock()
	defer func() { t.mu.Lock(); delete(t.pending, f.ID); t.mu.Unlock() }()
	select {
	case <-t.writeGate:
	case <-r.Context().Done():
		return nil, r.Context().Err()
	case <-t.closed:
		return nil, errors.New("product proxy disconnected")
	}
	if err := r.Context().Err(); err != nil {
		t.writeGate <- struct{}{}
		return nil, err
	}
	// Cancellation after admission may leave a partial frame. Detach this
	// stream to unblock Write; never retry or continue a damaged framing stream.
	stopWrite := context.AfterFunc(r.Context(), t.CloseIdleConnections)
	err := writeFrame(t.stream, f)
	stopWrite()
	t.writeGate <- struct{}{}
	if err != nil {
		t.CloseIdleConnections()
		return nil, err
	}
	select {
	case f := <-reply:
		if f.Status < 100 || f.Status > 599 || len(f.Body) > MaxResourceBytes {
			return nil, errors.New("invalid product proxy response")
		}
		return &http.Response{StatusCode: f.Status, Header: proxyHeaders(f.Headers), Body: io.NopCloser(bytes.NewReader(f.Body)), Request: r}, nil
	case <-r.Context().Done():
		return nil, r.Context().Err()
	case <-t.closed:
		return nil, errors.New("product proxy disconnected")
	}
}

func proxyHeaders(in http.Header) http.Header {
	out := make(http.Header)
	for _, key := range []string{"Content-Type", "X-Product-Bot", "X-Product-Generation", "X-Resource-Name", "X-Resource-Size", "X-Resource-SHA256"} {
		if value := in.Get(key); value != "" {
			out.Set(key, value)
		}
	}
	return out
}

func validProxyRequest(f proxyFrame) bool {
	if f.Status != 0 {
		return false
	}
	u, err := url.ParseRequestURI(f.Path)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" {
		return false
	}
	valid := false
	limit := MaxCommandBytes
	switch f.Method {
	case "GET":
		valid = u.Path == "/v1/identity" && u.RawQuery == ""
		if u.Path == "/v1/resources" {
			valid = len(u.Query()) == 1 && identifier.MatchString(u.Query().Get("id"))
		}
	case "POST":
		valid = u.RawQuery == "" && (u.Path == "/v1/state" || u.Path == "/v1/watch" || u.Path == "/v1/commands" || u.Path == "/v1/receipt" || managementPath(u.Path))
	case "PUT":
		valid = u.Path == "/v1/resources" && u.RawQuery == ""
		limit = MaxResourceBytes
	}
	if !valid || len(f.Body) > limit || len(f.Headers) > 6 {
		return false
	}
	for key, values := range f.Headers {
		if len(values) != 1 || len(values[0]) > 1024 || proxyHeaders(http.Header{key: values}).Get(key) == "" {
			return false
		}
	}
	return true
}

// ProxyStdio is the fixed target-local helper. Endpoint is a native loopback
// product listener; credentials are attached here, never returned to the client.
func ProxyStdio(ctx context.Context, in io.Reader, out io.Writer, endpoint, token string) error {
	check, err := NewClient(ClientOptions{URL: endpoint, ExpectedNode: "proxy-validation", ExpectedBot: "proxy-validation", Token: token})
	if err != nil {
		return err
	}
	defer check.Close()
	ctx, cancel := context.WithCancel(ctx)
	var writeMu sync.Mutex
	var workers sync.WaitGroup
	stopClose := context.AfterFunc(ctx, func() {
		if closer, ok := in.(io.Closer); ok {
			_ = closer.Close()
		}
		if closer, ok := out.(io.Closer); ok {
			_ = closer.Close()
		}
	})
	defer func() { cancel(); stopClose(); workers.Wait() }()
	semaphore := make(chan struct{}, 8)
	for {
		f, err := readFrame(in)
		if err != nil {
			cancel()
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if !validProxyRequest(f) {
			return errors.New("product proxy endpoint unavailable")
		}
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		workers.Add(1)
		go func(f proxyFrame) {
			defer workers.Done()
			defer func() { <-semaphore }()
			r, err := http.NewRequestWithContext(ctx, f.Method, check.options.URL+f.Path, bytes.NewReader(f.Body))
			response := proxyFrame{ID: f.ID, Status: 502, Body: []byte(`{"code":"product-unavailable"}`)}
			if err == nil {
				r.Header = proxyHeaders(f.Headers)
				r.Header.Set("Authorization", "Bearer "+token)
				h, err := check.http.Do(r)
				if err == nil {
					limit := MaxSnapshotBytes
					if f.Method == "GET" && strings.HasPrefix(f.Path, "/v1/resources?") {
						limit = MaxResourceBytes
					}
					b, readErr := io.ReadAll(io.LimitReader(h.Body, int64(limit)+1))
					_ = h.Body.Close()
					if readErr == nil && len(b) <= limit {
						response.Status = h.StatusCode
						response.Headers = proxyHeaders(h.Header)
						response.Body = b
					}
				}
			}
			writeMu.Lock()
			err = writeFrame(out, response)
			writeMu.Unlock()
			if err != nil {
				cancel()
			}
		}(f)
	}
}
