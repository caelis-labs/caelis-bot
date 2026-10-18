package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

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
			ctx := context.WithValue(t.Context(), invocationKey{}, wire.SessionToolCall{SessionId: "main", PrincipalId: "owner", Source: wire.SessionSource{Kind: "user", OperationId: "source"}})
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
		if r.URL.Path != "/api/control/v1/sessions/main/prompt" {
			t.Error("prompt retry changed operation", r.URL.Path)
		}
		posts.Add(1)
		var in wire.PromptRequest
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

func TestMainSteerLostResponseSettlesOriginalReceipt(t *testing.T) {
	for _, outcome := range []wire.Outcome{"committed", "rejected"} {
		for _, attached := range []bool{false, true} {
			t.Run(string(outcome)+map[bool]string{false: "/text", true: "/attachment"}[attached], func(t *testing.T) {
				var posts atomic.Int32
				s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "POST" {
						if !strings.HasSuffix(r.URL.Path, "/steer") {
							t.Errorf("unexpected mutation: %s", r.URL.Path)
						}
						posts.Add(1)
						drop(w)
						return
					}
					if !strings.HasSuffix(r.URL.Path, "/operations/original-steer") {
						t.Errorf("changed receipt: %s", r.URL.Path)
					}
					writeFixture(w, wire.CommandResult{OperationId: "original-steer", Outcome: outcome})
				})
				s.state.Views["main"].State.Run = wire.RunState{Active: pointer(true), Status: pointer("running"), HandleId: pointer("handle"), RunId: pointer("run"), TurnId: pointer("turn-original")}
				var files []api.InputFile
				if attached {
					path := filepath.Join(t.TempDir(), "image.png")
					if err := os.WriteFile(path, []byte("fixture image"), 0600); err != nil {
						t.Fatal(err)
					}
					files = []api.InputFile{{Path: path, Name: "image.png"}}
				}
				in := api.Submission{ID: "original-steer", Text: "guide"}
				receipt, err := s.Submit(t.Context(), in, files)
				if err != nil || receipt.Outcome != "unknown" {
					t.Fatal(receipt, err)
				}
				if err := s.recoverOperations(t.Context()); err != nil {
					t.Fatal(err)
				}
				got := s.Snapshot().LastReceipt
				if got.ID != in.ID || got.Outcome != productOutcome(outcome) {
					t.Fatalf("composer receipt: %+v", got)
				}
				if posts.Load() != 1 {
					t.Fatal("recovery replayed mutation", posts.Load())
				}
				restored := New(Options{Directory: filepath.Dir(s.path)})
				if restored.state.LastReceipt != got {
					t.Fatal("settled receipt not durable")
				}
			})
		}
	}
}

func TestMainSteerRecoveryWinsLateTransportFailure(t *testing.T) {
	for _, outcome := range []wire.Outcome{"committed", "rejected"} {
		t.Run(string(outcome), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var posts atomic.Int32
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts.Add(1)
					close(entered)
					<-release
					drop(w)
					return
				}
				writeFixture(w, wire.CommandResult{OperationId: "late-steer", Outcome: outcome})
			})
			s.state.Views["main"].State.Run = wire.RunState{Active: pointer(true), Status: pointer("running"), HandleId: pointer("h"), RunId: pointer("r"), TurnId: pointer("t")}
			done := make(chan api.Receipt, 1)
			go func() {
				receipt, _ := s.Submit(t.Context(), api.Submission{ID: "late-steer", Text: "guide"}, nil)
				done <- receipt
			}()
			<-entered
			err := s.recoverOperation(t.Context(), "late-steer")
			close(release)
			if err != nil {
				t.Fatal(err)
			}
			receipt := <-done
			if receipt.Outcome != productOutcome(outcome) || s.Snapshot().LastReceipt.Outcome != receipt.Outcome || posts.Load() != 1 {
				t.Fatalf("late response downgraded original receipt: %+v", receipt)
			}
		})
	}
}
