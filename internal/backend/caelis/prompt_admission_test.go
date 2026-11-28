package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestControlPromptDeadlineAtConnectionAdmission(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reuse   bool
		expires bool
	}{
		{"new-before-deadline", false, false},
		{"new-expired-during-dial", false, true},
		{"reused-before-deadline", true, false},
		{"reused-expired-at-checkout", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "occurrence:connection-admission"
			var posts atomic.Int32
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/control/v1/warm":
					writeFixture(w, struct{}{})
				case r.Method == http.MethodPost && r.URL.Path == "/api/control/v1/sessions/main/prompt":
					var input wire.PromptRequest
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil || value(input.OperationId) != id {
						t.Error("incorrect prompt identity", err)
					}
					posts.Add(1)
					writeFixture(w, wire.CommandResult{OperationId: id, Outcome: "committed"})
				default:
					http.NotFound(w, r)
				}
			})
			tr := s.client.http.Transport.(*http.Transport)
			t.Cleanup(tr.CloseIdleConnections)
			if tc.reuse {
				if err := s.client.json(t.Context(), http.MethodGet, "/warm", nil, nil, "", ""); err != nil {
					t.Fatal(err)
				}
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var unblock sync.Once
			defer unblock.Do(func() { close(release) })
			wait := func() {
				close(entered)
				<-release
			}
			gotConn := make(chan bool, 1)
			trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { gotConn <- info.Reused }}
			if tc.reuse {
				trace.GetConn = func(string) { wait() }
			} else {
				dial := tr.DialContext
				tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					wait()
					return dial(ctx, network, addr)
				}
			}
			deadline := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
			var late atomic.Bool
			ctx := api.WithSystemDispatchDeadline(httptrace.WithClientTrace(t.Context(), trace), deadline, func() time.Time {
				if late.Load() {
					return deadline
				}
				return deadline.Add(-time.Minute)
			})
			msg := api.SystemMessage{ID: id, Text: "Clock", Source: api.SystemCallback, Grants: []string{"schedule:clock"}}
			type result struct {
				receipt api.Receipt
				err     error
			}
			done := make(chan result, 1)
			go func() {
				out, err := s.SubmitSystem(ctx, msg)
				done <- result{out, err}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("connection wait was not reached")
			}
			if posts.Load() != 0 {
				t.Fatal("native prompt overtook connection admission")
			}
			late.Store(tc.expires)
			unblock.Do(func() { close(release) })
			var out result
			select {
			case out = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("prompt did not settle")
			}
			select {
			case reused := <-gotConn:
				if reused != tc.reuse {
					t.Fatalf("connection reused=%v, want %v", reused, tc.reuse)
				}
			default:
				t.Fatal("transport did not acquire a connection")
			}
			if tc.expires {
				if out.receipt.ID != id || out.receipt.Outcome != "rejected" || !out.receipt.Deferred ||
					!errors.Is(out.err, api.ErrSystemDispatchExpired) || posts.Load() != 0 || !s.Snapshot().CanSend {
					t.Fatalf("expired prompt left a send or native fence: %+v, %v, posts=%d", out.receipt, out.err, posts.Load())
				}
				return
			}
			if out.err != nil || out.receipt.ID != id || out.receipt.Outcome != "accepted" || posts.Load() != 1 {
				t.Fatalf("live prompt did not send once: %+v, %v, posts=%d", out.receipt, out.err, posts.Load())
			}
		})
	}
}

func TestControlPromptDeadlineStopsWaitingForDial(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("expired dial sent a prompt")
		http.NotFound(w, r)
	})
	tr := s.client.http.Transport.(*http.Transport)
	t.Cleanup(tr.CloseIdleConnections)
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	tr.DialContext = func(context.Context, string, string) (net.Conn, error) {
		close(entered)
		// Even if a dial implementation is slow to return, prompt admission
		// can end without waiting for it or submitting the request.
		<-release
		return nil, errors.New("fixture dial ended")
	}
	ctx := api.WithSystemDispatchDeadline(t.Context(), time.Now().Add(time.Second), time.Now)
	done := make(chan error, 1)
	go func() {
		done <- s.client.json(ctx, http.MethodPost, "/sessions/main/prompt", struct{}{}, nil, "clock", "")
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("dial was not reached")
	}
	select {
	case err := <-done:
		if !errors.Is(err, api.ErrSystemDispatchExpired) {
			t.Fatalf("waiting dial lacked an undispatched expiry: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expiry waited for the blocked dial")
	}
}

func TestControlPromptAdmissionLeavesLateResponseObservable(t *testing.T) {
	for _, loss := range []bool{false, true} {
		name := "committed"
		if loss {
			name = "lost"
		}
		t.Run(name, func(t *testing.T) {
			const id = "occurrence:late-control-response"
			received, release := make(chan struct{}), make(chan struct{})
			var unblock sync.Once
			defer unblock.Do(func() { close(release) })
			var posts atomic.Int32
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if posts.Add(1) == 1 {
					close(received)
				}
				<-release
				if loss {
					drop(w)
					return
				}
				writeFixture(w, wire.CommandResult{OperationId: id, Outcome: "committed"})
			})
			// This context observes the clock only. The request has its usual
			// transport timeout while the server holds the response.
			until := time.Now().Add(time.Second)
			clock, stop := context.WithDeadline(t.Context(), until)
			defer stop()
			ctx := api.WithSystemDispatchDeadline(t.Context(), until, time.Now)
			var out wire.CommandResult
			done := make(chan error, 1)
			go func() {
				done <- s.client.json(ctx, http.MethodPost, "/sessions/main/prompt", struct{}{}, &out, id, "")
			}()
			select {
			case <-received:
			case <-clock.Done():
				t.Fatal("prompt did not reach the server within the test window")
			}
			<-clock.Done()
			unblock.Do(func() { close(release) })
			select {
			case err := <-done:
				if posts.Load() != 1 || errors.Is(err, api.ErrSystemDispatchExpired) {
					t.Fatal("a sent prompt was retried or relabelled as unsent", posts.Load(), err)
				}
				if loss && err == nil {
					t.Fatal("lost response must remain unconfirmed")
				}
				if !loss && (err != nil || out.OperationId != id || out.Outcome != "committed") {
					t.Fatal("late response did not settle the original request", out, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("late response observation did not finish")
			}
		})
	}
}
