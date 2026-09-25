package caelis

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestCommandObservationIsOptional(t *testing.T) {
	var unexpected atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/initialize") {
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: required})
			return
		}
		unexpected.Add(1)
		w.WriteHeader(http.StatusForbidden)
	})
	var err error
	s.info, err = initialize(t.Context(), s.client)
	if err != nil {
		t.Fatal("baseline Host rejected without optional capability", err)
	}
	s.trackCommandApprovalLocked("main", "old-host-command", "execute")
	if len(s.state.CommandFollowups) != 0 {
		t.Fatal("unsupported Host enrolled a command observer")
	}
	// A pending record can survive reconnect from a newer Host to an older one.
	s.info.Capabilities = append(append([]string{}, required...), commandObservationCapability)
	s.trackCommandApprovalLocked("main", "retained-command", "execute")
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	s.state, err = loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.info.Capabilities = required
	for range 3 {
		if err := s.reportApprovedCommands(t.Context()); err != nil {
			t.Fatal("optional observation broke the baseline connection", err)
		}
	}
	if unexpected.Load() != 0 || !s.Snapshot().CanSend {
		t.Fatal("unsupported observation requested an endpoint or blocked normal work")
	}
	if f, ok := s.state.CommandFollowups["retained-command"]; !ok || f.Done {
		t.Fatal("capability loss erased retained completion evidence")
	}
}

func TestApprovedCommandFinishesAfterModelTurn(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "unknown"}[lost], func(t *testing.T) {
			var reads, posts atomic.Int32
			var current atomic.Value
			current.Store(wire.TaskStateRunning)
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/tasks") {
					state := current.Load().(wire.TaskState)
					reads.Add(1)
					writeFixture(w, wire.TaskList{Tasks: []wire.TaskDescriptor{
						{SessionId: "foreign", Kind: "command", TaskId: "wrong", Handle: "wrong", State: "completed", ParentTool: &wire.TaskParentTool{ToolCallId: pointer("call")}},
						{SessionId: "main", Kind: "command", TaskId: "command-id", Handle: "command-1", State: state, Running: state == "running", ParentTool: &wire.TaskParentTool{ToolCallId: pointer("call")}},
					}})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/terminals/output") {
					var req wire.TerminalRequest
					_ = json.NewDecoder(r.Body).Decode(&req)
					if req.SessionId != "main" || req.TerminalId != "command-id" {
						t.Error("terminal read lost exact task")
					}
					writeFixture(w, wire.TerminalOutput{})
					return
				}
				if !strings.HasSuffix(r.URL.Path, "/prompt") {
					t.Errorf("unexpected effect %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				posts.Add(1)
				var req wire.ApplicationPromptRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				if req.SourceKind != "application_summary" || !strings.Contains(value(req.Input), `handle "command-1"`) || strings.Contains(value(req.Input), `handle "wrong"`) {
					t.Error("notice lost authority/identity")
				}
				if lost {
					drop(w)
					return
				}
				writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "accepted"})
			})
			s.info.Capabilities = []string{commandObservationCapability}
			s.trackCommandApprovalLocked("main", "call", "execute")
			s.state.Views["main"].State.Run.Active = pointer(true)
			if err := s.reportApprovedCommands(t.Context()); err != nil || reads.Load() != 0 {
				t.Fatal("polled while model active", err)
			}
			s.state.Views["main"].State.Run.Active = pointer(false)
			for range 2 {
				if err := s.reportApprovedCommands(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if posts.Load() != 0 {
				t.Fatal("woke model before command completed")
			}
			current.Store(wire.TaskStateCompleted)
			if err := s.reportApprovedCommands(t.Context()); err != nil {
				t.Fatal(err)
			}
			if posts.Load() != 1 {
				t.Fatal("late completion was orphaned")
			}
			restored, err := loadBinding(s.path)
			if err != nil {
				t.Fatal(err)
			}
			s.state = restored
			for range 3 {
				_ = s.reportApprovedCommands(t.Context())
			}
			if posts.Load() != 1 {
				t.Fatal("completion duplicated after restart or uncertain response")
			}
		})
	}
}
