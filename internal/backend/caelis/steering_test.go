package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestSharedNativeSteeringKeepsExpectedTurnOnDuplicate(t *testing.T) {
	for _, workerMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "main", true: "worker"}[workerMode], func(t *testing.T) {
			var posts atomic.Int32
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/steer") {
					t.Error("steering became another prompt", r.URL.Path)
				}
				posts.Add(1)
				var in wire.SteerRequest
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				if in.Target.TurnId != "turn-original" {
					t.Error("steering retargeted", in.Target)
				}
				writeFixture(w, wire.CommandResult{OperationId: value(in.OperationId), Outcome: "committed", InputStatus: pointer("accepted")})
			})
			s.state.PrincipalID = "owner"
			s.state.Views["main"].State.Run = wire.RunState{Active: pointer(true), Status: pointer("running"), HandleId: pointer("handle"), RunId: pointer("run"), TurnId: pointer("turn-original")}
			s.state.Workers["task"] = worker{Native: true, Binding: s.state.Session, Task: api.Task{ID: "task"}}
			ctx := context.WithValue(t.Context(), invocationKey{}, wire.ApplicationCall{SessionId: "main", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", Source: wire.ApplicationSource{Kind: "user", OperationId: "source"}})
			send := func(s *Session, text string) {
				t.Helper()
				if workerMode {
					v, e := s.SendWork(ctx, api.TaskMessage{ID: "task", RequestID: "same-id", Prompt: text})
					if e != nil || v.Outcome != "accepted" {
						t.Fatalf("worker steer: %+v %v", v, e)
					}
				} else {
					v, e := s.Submit(ctx, api.Submission{ID: "same-id", Text: text}, nil)
					if e != nil || v.Outcome != "accepted" {
						t.Fatalf("main steer: %+v %v", v, e)
					}
				}
			}
			if !s.Snapshot().CanSteer {
				t.Fatal("main steering not advertised")
			}
			send(s, "guide")
			restored := New(Options{Directory: filepath.Dir(s.path)})
			restored.client = s.client
			restored.connected = true
			restored.state.Views["main"].State.Run.TurnId = pointer("turn-new")
			restored.state.Views["main"].State.Run.Active = pointer(false)
			send(restored, "guide")
			if workerMode {
				if _, err := restored.SendWork(ctx, api.TaskMessage{ID: "task", RequestID: "same-id", Prompt: "changed payload"}); err == nil {
					t.Fatal("changed steering accepted")
				}
			} else if receipt, err := restored.Submit(ctx, api.Submission{ID: "same-id", Text: "changed payload"}, nil); err != nil || receipt.Outcome != "rejected" || !strings.Contains(receipt.Message, "不同消息") {
				t.Fatal("changed steering accepted")
			}
			if posts.Load() != 1 {
				t.Fatal("duplicate steering dispatched twice")
			}
		})
	}
}

func TestSharedWorkerProgressNeedsNoStatePollingAndNoticeIsNotFailure(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("steady subscription polled") })
	s.state.Connection.ExpiresAt = time.Now().Add(time.Hour)
	s.streams["main"] = true
	s.state.Views["main"].State.Run = wire.RunState{Active: pointer(true), Status: pointer("running"), HandleId: pointer("h"), RunId: pointer("r"), TurnId: pointer("t")}
	applyEnvelope(s.state.Views["main"], wire.Envelope{Kind: "caelis/notice", Notice: pointer("optional integration unavailable")})
	if s.needsRefreshLocked() || s.Snapshot().Phase != "working" {
		t.Fatal("notice became failure or progress requires polling")
	}
}

func TestMainPromptRetryWhileBusyKeepsOriginalOperation(t *testing.T) {
	var posts atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/control/v1/application/sessions/main/prompt" {
			t.Error("prompt retry changed operation", r.URL.Path)
		}
		posts.Add(1)
		var in wire.ApplicationPromptRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		writeFixture(w, wire.CommandResult{OperationId: value(in.OperationId), Outcome: "committed"})
	})
	in := api.Submission{ID: "original-prompt", Text: "first instruction"}
	if v, e := s.Submit(t.Context(), in, nil); e != nil || v.Outcome != "accepted" {
		t.Fatalf("initial: %+v %v", v, e)
	}
	s.state.Views["main"].State.Run = wire.RunState{Active: pointer(true), HandleId: pointer("h"), RunId: pointer("r"), TurnId: pointer("t")}
	if v, e := s.Submit(t.Context(), in, nil); e != nil || v.Outcome != "accepted" {
		t.Fatalf("busy retry: %+v %v", v, e)
	}
	if posts.Load() != 1 {
		t.Fatal("retry dispatched an additional operation")
	}
}
