package codex

import (
	"context"
	"net"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestSharedReconnectRecoversAfterInterruptedTrustRead(t *testing.T) {
	s, observe := trustSessionFixture(t, "trusted")
	s.binding.OwnerEndpoint = "unix://" + filepath.Join(s.opts.Directory, "app-server-control", "app-server-control.sock")
	s.binding.ThreadID = "resident"
	s.reconnectDelay = func(int) time.Duration { return time.Millisecond }

	foreground, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	configRead := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	first := true
	healthy := s.start
	s.start = func(ctx context.Context, opts Options) (*Client, error) {
		if !first {
			return healthy(ctx, opts)
		}
		first = false
		client, server := net.Pipe()
		fixture := &sessionFixture{handle: func(m wireMessage) (any, bool) {
			if m.Method == "config/read" {
				close(configRead)
				cancel()
				<-release // The caller stopped waiting before the first read replied.
				return nil, true
			}
			return nil, false
		}}
		go fixture.serve(server)
		return &Client{rpc: newTransportOptions(client, nil, true)}, nil
	}
	if err := s.Connect(foreground); err == nil {
		t.Fatal("interrupted trust read unexpectedly succeeded")
	}
	select {
	case <-configRead:
	default:
		t.Fatal("foreground recovery did not reach the workspace trust read")
	}

	ctx, stop := context.WithTimeout(t.Context(), 2*time.Second)
	defer stop()
	for snapshot := s.Snapshot(); snapshot.Connection != "ready"; {
		next, err := s.WaitSnapshot(ctx, snapshot.Revision)
		if err != nil {
			t.Fatalf("original connection did not recover after the interrupted read: issue=%s", snapshot.ConnectionIssue)
		}
		snapshot = next
	}
	writes, methods := observe()
	if writes != 0 || !slices.Contains(methods, "config/read") || !slices.Contains(methods, "thread/resume") || slices.Contains(methods, "thread/start") {
		t.Fatalf("recovery must reread trust and resume the original thread: %v", methods)
	}
}
