package caelis

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestWorkerObservationRetriesOnlySameOwnerProjection(t *testing.T) {
	for _, change := range []string{"once", "continuous", "binding", "receipt", "generation", "cancelled", "cancelled_response"} {
		t.Run(change, func(t *testing.T) {
			var gets atomic.Int32
			var s *Session
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s = fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
					t.Errorf("unexpected effect or owner: %s %s", r.Method, r.URL.Path)
					return
				}
				n := gets.Add(1)
				s.mu.Lock()
				current := s.state.Workers["task"]
				switch change {
				case "once":
					if n == 1 {
						s.state.Views["work"].Observed++
					}
				case "continuous":
					s.state.Views["work"].Observed++
				case "binding":
					current.Binding.PrincipalId = "replacement"
				case "receipt":
					current.PromptID = "replacement"
				case "generation":
					s.generation++
				case "cancelled":
					cancel()
				}
				s.state.Workers["task"] = current
				s.mu.Unlock()
				writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("completed"), Active: pointer(false), TurnId: pointer("turn")}})
			})
			if change == "cancelled_response" {
				s.client.http.Transport = workerCancelledResponseTransport{next: s.client.http.Transport, cancel: cancel}
			}
			s.state.Workers["task"] = worker{Binding: wire.SessionBinding{SessionId: "work", PrincipalId: "owner"}, Task: api.Task{ID: "task", Status: "completed", Outcome: "accepted"}, PromptID: "original", Activity: "idle"}
			s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("completed"), Active: pointer(false), TurnId: pointer("turn")}}, Seen: map[string]bool{}}
			got, err := s.ObserveWork(ctx, "task")
			wantReads := int32(1)
			if change == "once" || change == "continuous" {
				wantReads = 2
			}
			if gets.Load() != wantReads {
				t.Fatalf("unbounded retry or changed owner read: %d", gets.Load())
			}
			if change == "once" {
				if err != nil || got.State.Activity != "idle" || api.TaskFinishToken(got.State) == "" {
					t.Fatal("same-owner read did not recover", got, err)
				}
			} else {
				if err == nil || api.TaskFinishToken(got.State) != "" || s.state.Workers["task"].Activity != "" {
					t.Fatal("failed observation issued finish proof", got, err)
				}
				if errors.Is(err, api.ErrWorkObservationChanged) != (change == "continuous") {
					t.Fatal("projection movement confused with owner failure", err)
				}
			}
		})
	}
}

// A completed response can race cancellation and still decode successfully.
// Make that ordering deterministic instead of depending on net/http scheduling.
type workerCancelledResponseTransport struct {
	next   http.RoundTripper
	cancel context.CancelFunc
}

func (tr workerCancelledResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := tr.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		return nil, err
	}
	tr.cancel()
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

// Cancellation after a fully decoded response must still precede publication
// of an idle/finish proof. Closing the successful body fixes that ordering.
type cancelReadBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b cancelReadBody) Close() error { b.cancel(); return b.ReadCloser.Close() }

type cancelReadTransport struct {
	http.RoundTripper
	cancel context.CancelFunc
}

func (tr cancelReadTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := tr.RoundTripper.RoundTrip(r)
	if err == nil {
		response.Body = cancelReadBody{ReadCloser: response.Body, cancel: tr.cancel}
	}
	return response, err
}
func TestCancelledSuccessfulWorkerReadCannotPublishFinishProof(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var gets atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
			t.Errorf("unexpected owner/effect: %s %s", r.Method, r.URL.Path)
			return
		}
		gets.Add(1)
		writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("completed"), Active: pointer(false), TurnId: pointer("turn")}})
	})
	s.client.http.Transport = cancelReadTransport{RoundTripper: s.client.http.Transport, cancel: cancel}
	s.state.Workers["task"] = worker{Binding: wire.SessionBinding{SessionId: "work", PrincipalId: "owner"}, Task: api.Task{ID: "task", Status: "completed", Outcome: "accepted"}, PromptID: "original", Activity: "idle"}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("completed"), Active: pointer(false), TurnId: pointer("turn")}}, Seen: map[string]bool{}}
	got, err := s.ObserveWork(ctx, "task")
	if !errors.Is(err, context.Canceled) || api.TaskFinishToken(got.State) != "" || s.state.Workers["task"].Activity != "" || gets.Load() != 1 {
		t.Fatal("cancelled successful read published idle/finish proof", got, err, gets.Load())
	}
}
