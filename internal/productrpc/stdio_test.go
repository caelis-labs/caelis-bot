package productrpc

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"
)

type capturedStream struct {
	net.Conn
	mu      sync.Mutex
	written bytes.Buffer
}

func (s *capturedStream) Write(b []byte) (int, error) {
	s.mu.Lock()
	s.written.Write(b)
	s.mu.Unlock()
	return s.Conn.Write(b)
}

func TestTargetLocalStdioAuthAndConcurrentWatchCommand(t *testing.T) {
	_, _, f, h, opts := fixture(t)
	local, remote := net.Pipe()
	capture := &capturedStream{Conn: local}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- ProxyStdio(ctx, remote, remote, h.URL, opts.Token) }()
	c, err := NewStdioClient(StdioOptions{ExpectedNode: opts.NodeID, ExpectedBot: opts.BotID}, capture)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if _, err = c.Connect(bounded(t)); err != nil {
		t.Fatal(err)
	}
	state, err := c.State(bounded(t))
	if err != nil {
		t.Fatal(err)
	}
	watch := make(chan error, 1)
	go func() { _, err := c.Watch(bounded(t), state.Cursor); watch <- err }()
	if r, err := c.Command(bounded(t), Command{ID: "stdio-command", Kind: "load-earlier"}); err != nil || r.Outcome != "accepted" {
		t.Fatalf("command blocked by watch: %+v %v", r, err)
	}
	select {
	case err := <-watch:
		if err != nil {
			t.Fatal(err)
		}
	case <-bounded(t).Done():
		t.Fatal("watch did not observe command")
	}
	capture.mu.Lock()
	leaked := bytes.Contains(capture.written.Bytes(), []byte(opts.Token))
	capture.mu.Unlock()
	if leaked {
		t.Fatal("target product token entered client stream")
	}
	c.Close()
	select {
	case <-done:
	case <-bounded(t).Done():
		t.Fatal("proxy did not detach")
	}
	if f.stops != 0 {
		t.Fatal("stdio EOF stopped resident Bot")
	}
}

func TestProxyRefusesArbitraryPathsHeadersAndOversizedFrames(t *testing.T) {
	for _, f := range []proxyFrame{
		{ID: "1", Method: "GET", Path: "http://example.com/v1/identity"},
		{ID: "1", Method: "GET", Path: "/v1/identity?redirect=1"},
		{ID: "1", Method: "POST", Path: "/v1/reflection"},
		{ID: "1", Method: "GET", Path: "/v1/identity", Headers: map[string][]string{"Authorization": {"Bearer forbidden"}}},
		{ID: "1", Method: "GET", Path: "/v1/resources?id=../private"},
	} {
		if validProxyRequest(f) {
			t.Fatalf("unsafe proxy frame allowed: %s", f.Path)
		}
	}
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], maxProxyFrame+1)
	if _, err := readFrame(bytes.NewReader(prefix[:])); err == nil {
		t.Fatal("oversized frame accepted")
	}
}

type blockedProxyStream struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

func (s *blockedProxyStream) Write(b []byte) (int, error) {
	s.once.Do(func() { close(s.entered) })
	return s.Conn.Write(b)
}

func TestStdioCanceledWriterAndQueuedRequestDetachWithoutRetry(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()
	stream := &blockedProxyStream{Conn: local, entered: make(chan struct{})}
	c, err := NewStdioClient(StdioOptions{ExpectedNode: "node", ExpectedBot: "bot"}, stream)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	firstCtx, cancelFirst := context.WithCancel(t.Context())
	defer cancelFirst()
	first := make(chan error, 1)
	go func() { _, err := c.Connect(firstCtx); first <- err }()
	select {
	case <-stream.entered:
	case <-bounded(t).Done():
		t.Fatal("writer not admitted")
	}
	queuedCtx, cancelQueued := context.WithCancel(t.Context())
	queued := make(chan error, 1)
	go func() { _, err := c.Connect(queuedCtx); queued <- err }()
	cancelQueued()
	select {
	case err := <-queued:
		if err == nil {
			t.Fatal("queued cancellation accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("queued request ignored cancellation")
	}
	cancelFirst()
	select {
	case err := <-first:
		if err == nil {
			t.Fatal("partial write cancellation accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("admitted writer ignored cancellation")
	}
	if _, err = c.Connect(t.Context()); err == nil {
		t.Fatal("damaged stream continued after cancellation")
	}
}
