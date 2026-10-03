package caelis

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestStreamAllowsSlowReconnectWithoutRelaxingJSONTimeout(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/control/v1/reconnect" {
			<-r.Context().Done()
			return
		}
		if r.Header.Get("Authorization") != "Bearer TEST_TOKEN" || r.Header.Get("Accept") != "text/event-stream" || r.Header.Get("Last-Event-ID") != "original-cursor" {
			t.Error("stream lost its original authentication or cursor")
		}
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: ready\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	c, err := newClient(server.URL, "TEST_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	original := c.http.Transport.(*http.Transport)
	original.ResponseHeaderTimeout = 50 * time.Millisecond
	t.Cleanup(c.http.CloseIdleConnections)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	observed := errors.New("observed original stream")
	done := make(chan error, 1)
	go func() {
		done <- c.stream(ctx, "/reconnect", "original-cursor", func(f frame) error {
			if f.event != "ready" {
				t.Error("lost delayed bootstrap frame")
			}
			return observed
		})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	// The reconnect remains pending across several ordinary header deadlines.
	// JSON must still time out while the separately owned SSE request can finish.
	for range 3 {
		if err := c.json(t.Context(), "GET", "/status", nil, nil, "", ""); err == nil {
			t.Fatal("ordinary request lost its timeout")
		}
	}
	if original.ResponseHeaderTimeout != 50*time.Millisecond {
		t.Fatal("stream changed the shared transport")
	}
	unblock()
	select {
	case err := <-done:
		if !errors.Is(err, observed) {
			t.Fatalf("slow reconnect was mistaken for disconnection: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("delayed stream did not deliver its original frame")
	}
}

func TestStreamHeaderWaitHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	c, err := newClient(server.URL, "TEST_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.stream(ctx, "/reconnect", "", func(frame) error { return nil }) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled observation reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("header wait prevented observation teardown")
	}
}

func TestRepeatedConnectionFailureKeepsOneOfflineTransition(t *testing.T) {
	s := New(Options{Directory: t.TempDir()})
	s.connected = true
	err := errors.New("same unavailable connection")
	s.fail(err)
	first := s.Snapshot()
	s.fail(err)
	repeated := s.Snapshot()
	if first.Connection != "offline" || repeated.Revision != first.Revision || repeated.CanSend {
		t.Fatal("unchanged failure republished an offline transition")
	}
	s.fail(errors.New("different recovery issue"))
	if s.Snapshot().Revision == first.Revision {
		t.Fatal("changed connection issue was hidden")
	}
	s.connected = true
	s.issue = err.Error()
	recovered := s.Snapshot().Revision
	s.fail(err)
	if s.Snapshot().Revision == recovered {
		t.Fatal("a new disconnection after recovery was hidden")
	}
}
