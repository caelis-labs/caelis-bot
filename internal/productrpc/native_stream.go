package productrpc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
)

// NativeStreamTransport reuses the product framed protocol for closed optional
// native planes. The caller owns private IPC/SSH authentication and the exact
// method/path allowlist. No bearer token or arbitrary URL enters this stream.
type NativeStreamTransport interface {
	http.RoundTripper
	CloseIdleConnections()
}

func nativeRequest(f proxyFrame, allow func(string, string) bool) bool {
	return f.Status == 0 && len(f.Headers) == 0 && len(f.Body) <= MaxCommandBytes && allow != nil && allow(f.Method, f.Path)
}

func NewNativeStreamTransport(stream io.ReadWriteCloser, allow func(string, string) bool) (NativeStreamTransport, error) {
	if stream == nil || allow == nil {
		return nil, errors.New("native stream and closed endpoint allowlist required")
	}
	t := &stdioTransport{stream: stream, pending: make(map[string]chan proxyFrame), closed: make(chan struct{}), writeGate: make(chan struct{}, 1), validate: func(f proxyFrame) bool { return nativeRequest(f, allow) }}
	t.writeGate <- struct{}{}
	go t.read()
	return t, nil
}

// ServeNativeStream dispatches only allowlisted framed requests to a local
// typed handler. Stream EOF cancels observations; admitted mutations must use
// their original journal independently of this observation context.
func ServeNativeStream(ctx context.Context, in io.Reader, out io.Writer, handler http.Handler, allow func(string, string) bool) error {
	if handler == nil || allow == nil {
		return errors.New("native typed handler required")
	}
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	var writeMu sync.Mutex
	stop := context.AfterFunc(ctx, func() {
		if c, ok := in.(io.Closer); ok {
			_ = c.Close()
		}
		if c, ok := out.(io.Closer); ok {
			_ = c.Close()
		}
	})
	defer func() { cancel(); stop(); workers.Wait() }()
	gate := make(chan struct{}, 8)
	for {
		f, err := readFrame(in)
		if err != nil {
			cancel()
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if !nativeRequest(f, allow) {
			return errors.New("native endpoint unavailable")
		}
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		workers.Add(1)
		go func(f proxyFrame) {
			defer workers.Done()
			defer func() { <-gate }()
			r, err := http.NewRequestWithContext(ctx, f.Method, "http://127.0.0.1"+f.Path, bytes.NewReader(f.Body))
			response := proxyFrame{ID: f.ID, Status: 502, Body: []byte(`{"code":"native-unavailable"}`)}
			if err == nil {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, r)
				if recorder.Body.Len() <= MaxSnapshotBytes {
					response.Status = recorder.Code
					response.Body = recorder.Body.Bytes()
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
