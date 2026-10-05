package caelis

import (
	"context"
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
	s.state.Views["main"].CommandCaughtUp = true
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
				if req.SourceKind != "application_summary" || value(req.Input) != "Command command-1 is completed." {
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
			s.state.Views["main"].CommandCaughtUp = true
			commandEvidenceFixture(s, "waiting_approval", true)
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
			s.state.Views["main"].CommandCaughtUp = true
			for range 3 {
				_ = s.reportApprovedCommands(t.Context())
			}
			if posts.Load() != 1 {
				t.Fatal("completion duplicated after restart or uncertain response")
			}
		})
	}
}

func commandEvidenceFixture(s *Session, state string, ended bool) {
	applyEnvelope(s.state.Views["main"], commandResultEnvelope("RunCommand", "call", "", map[string]any{"handle": "command-1", "state": state}))
	if ended {
		applyEnvelope(s.state.Views["main"], wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("original"), Delivery: wire.Delivery{Mode: "canonical"}, Lifecycle: &wire.LifecycleEvent{State: "completed"}})
	}
}
func commandResultEnvelope(name, call, action string, output map[string]any) wire.Envelope {
	raw, _ := json.Marshal(map[string]any{"sessionUpdate": "tool_call_update", "name": name, "toolCallId": call, "rawInput": map[string]string{"action": action}, "rawOutput": output})
	u := wire.ACPUpdate(raw)
	return wire.Envelope{Kind: "session/update", TurnId: pointer("original"), Delivery: wire.Delivery{Mode: "canonical"}, Update: &u}
}
func TestCommandFollowupRejectedIsVisibleNotDelivered(t *testing.T) {
	for _, outcome := range []wire.Outcome{"rejected", "conflicted", "committed"} {
		t.Run(string(outcome), func(t *testing.T) {
			posts := 0
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/tasks") {
					writeFixture(w, wire.TaskList{Tasks: []wire.TaskDescriptor{{SessionId: "main", Kind: "command", TaskId: "command-id", Handle: "command-1", State: "completed", ParentTool: &wire.TaskParentTool{ToolCallId: pointer("call")}}}})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/prompt") {
					posts++
					var req wire.ApplicationPromptRequest
					_ = json.NewDecoder(r.Body).Decode(&req)
					writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: outcome})
					return
				}
				t.Errorf("unexpected request: %s", r.URL.Path)
			})
			s.info.Capabilities = []string{commandObservationCapability}
			s.trackCommandApprovalLocked("main", "call", "execute")
			s.state.Views["main"].CommandCaughtUp = true
			commandEvidenceFixture(s, "waiting_approval", true)
			for range 3 {
				if err := s.reportApprovedCommands(t.Context()); err != nil {
					t.Fatal(err)
				}
				var err error
				s.state, err = loadBinding(s.path)
				s.state.Views["main"].CommandCaughtUp = true
				if err != nil {
					t.Fatal(err)
				}
			}
			f := s.state.CommandFollowups["call"]
			if outcome == "committed" {
				if !f.Done {
					t.Fatal("committed result not completed")
				}
			} else if f.Done || f.DeliveryFailure != string(outcome) {
				t.Fatalf("lost failed delivery: %+v", f)
			}
			if posts != 1 {
				t.Fatalf("unexpected retry: %d", posts)
			}
			if outcome != "committed" {
				if s.Snapshot().Message == "" || !s.Snapshot().CanSend {
					t.Fatal("nondelivery hidden from chat status or blocked recovery input")
				}
				applyEnvelope(s.state.Views["main"], commandResultEnvelope("Task", "read-recovery", "read", map[string]any{"handle": "command-1", "state": "completed"}))
				if s.Snapshot().Message != "" {
					t.Fatal("observed result did not clear failure status")
				}
			}
		})
	}
}
func TestCommandFollowupRequiresUnconsumedCanonicalResult(t *testing.T) {
	for _, scenario := range []string{"direct", "read", "wait", "batch", "no-evidence", "turn-not-ended", "transient"} {
		t.Run(scenario, func(t *testing.T) {
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/tasks") {
					t.Errorf("extra model activation %s", r.URL.Path)
					return
				}
				writeFixture(w, wire.TaskList{Tasks: []wire.TaskDescriptor{{SessionId: "main", Kind: "command", TaskId: "command-id", Handle: "command-1", State: "completed", ParentTool: &wire.TaskParentTool{ToolCallId: pointer("call")}}}})
			})
			s.info.Capabilities = []string{commandObservationCapability}
			s.trackCommandApprovalLocked("main", "call", "execute")
			s.state.Views["main"].CommandCaughtUp = true
			if scenario == "direct" {
				commandEvidenceFixture(s, "completed", true)
			}
			if scenario == "turn-not-ended" {
				commandEvidenceFixture(s, "running", false)
				s.state.Views["main"].State.Run.Active = pointer(false)
			}
			if scenario == "transient" {
				e := commandResultEnvelope("RunCommand", "call", "", map[string]any{"handle": "command-1", "state": "running"})
				e.Delivery.Mode = "transient"
				applyEnvelope(s.state.Views["main"], e)
			}
			if scenario == "read" || scenario == "wait" || scenario == "batch" {
				commandEvidenceFixture(s, "running", false)
				output := map[string]any{"handle": "command-1", "state": "completed"}
				action := scenario
				if scenario == "batch" {
					output = map[string]any{"tasks": []any{output}}
					action = "wait"
				}
				applyEnvelope(s.state.Views["main"], commandResultEnvelope("Task", "observer", action, output))
				commandEvidenceFixture(s, "running", true)
			}
			if err := s.saveLocked(); err != nil {
				t.Fatal(err)
			}
			var err error
			s.state, err = loadBinding(s.path)
			s.state.Views["main"].CommandCaughtUp = true
			if err != nil {
				t.Fatal(err)
			}
			for range 3 {
				if err := s.reportApprovedCommands(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestCommandFollowupWaitsForRecoverySyncAndCanonicalEvidence(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("recovery dispatched %s", r.URL.Path) })
	s.info.Capabilities = []string{commandObservationCapability}
	s.trackCommandApprovalLocked("main", "call", "execute")
	commandEvidenceFixture(s, "running", true)
	if err := s.reportApprovedCommands(t.Context()); err != nil {
		t.Fatal(err)
	}
	v := s.state.Views["main"]
	for _, mode := range []wire.DeliveryMode{"transient", "mirror"} {
		e := commandResultEnvelope("Task", "observer", "read", map[string]any{"handle": "command-1", "state": "completed"})
		e.Delivery.Mode = mode
		applyEnvelope(v, e)
	}
	e := commandResultEnvelope("Task", "foreign", "read", map[string]any{"handle": "command-1", "state": "completed"})
	e.Scope = pointer("participant")
	e.ParticipantId = pointer("worker")
	applyEnvelope(v, e)
	if v.CommandResults["call"].Received {
		t.Fatal("noncanonical or foreign result counted as delivery")
	}
}
func TestCommandFollowupMigrationRepairsRejectedDone(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected request") })
	s.state.ProjectionVersion = 6
	s.state.CommandFollowups = map[string]commandFollowup{"call": {Done: true}}
	op := "command-result-" + digest([]byte("main\x00call"))
	s.state.Operations[op] = journal{Outcome: "conflicted"}
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	b, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if f := b.CommandFollowups["call"]; f.Done || f.DeliveryFailure != "conflicted" {
		t.Fatalf("migration lost rejection: %+v", f)
	}
}

func TestCommandFollowupReconnectFencesBeforeBootstrap(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) { t.Error("canceled stream sent a request") })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s.mu.Lock()
	s.ctx = ctx
	s.state.Views["main"].CommandCaughtUp = true
	s.ensureStreamLocked("main")
	caughtUp := s.state.Views["main"].CommandCaughtUp
	s.mu.Unlock()
	s.wg.Wait()
	if caughtUp {
		t.Fatal("previous connection remained eligible before bootstrap")
	}
}

func TestCommandFollowupRecoveryOvertakesTerminalRead(t *testing.T) {
	var s *Session
	s = fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/tasks") {
			t.Errorf("dispatched during recovery: %s", r.URL.Path)
			return
		}
		s.mu.Lock()
		s.state.Views["main"].CommandCaughtUp = false
		s.mu.Unlock()
		writeFixture(w, wire.TaskList{Tasks: []wire.TaskDescriptor{{SessionId: "main", Kind: "command", TaskId: "command-id", Handle: "command-1", State: "completed", ParentTool: &wire.TaskParentTool{ToolCallId: pointer("call")}}}})
	})
	s.info.Capabilities = []string{commandObservationCapability}
	s.trackCommandApprovalLocked("main", "call", "execute")
	commandEvidenceFixture(s, "running", true)
	s.state.Views["main"].CommandCaughtUp = true
	if err := s.reportApprovedCommands(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s.state.CommandFollowups["call"].Done {
		t.Fatal("recovery race completed notification")
	}
}
