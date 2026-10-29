package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// A command can outlive the model turn that yielded while asking for approval.
// Track only native command identities associated with observed approvals. This
// is a finite completion notice, never a replay of the command or a new grant.
type commandFollowup struct {
	Done            bool   `json:"done,omitempty"`
	DeliveryFailure string `json:"deliveryFailure,omitempty"`
}

// This is optional Host observation support, not part of the Bot connection
// contract. Recheck the current handshake after reconnect before any request.
const commandObservationCapability = "session-terminal-observation-v1"

func (s *Session) trackCommandApprovalLocked(sid, callID, kind string) {
	if !slices.Contains(s.info.Capabilities, commandObservationCapability) || sid != s.state.Session.SessionId || callID == "" || kind != "execute" {
		return
	}
	if s.state.CommandFollowups == nil {
		s.state.CommandFollowups = map[string]commandFollowup{}
	}
	if _, exists := s.state.CommandFollowups[callID]; !exists {
		s.state.CommandFollowups[callID] = commandFollowup{}
	}
}

func (s *Session) observeCommandApprovalLocked(sid string, e wire.Envelope) {
	if e.Kind == "session/request_permission" && e.Permission != nil {
		s.trackCommandApprovalLocked(sid, e.Permission.ToolCall.ToolCallId, value(e.Permission.ToolCall.Kind))
	}
}

func (s *Session) observeApprovalHeadLocked(state wire.SessionState) {
	if a := state.Approval.Active; a != nil {
		b, _ := json.Marshal(a.Permission)
		var p nativePermission
		if json.Unmarshal(b, &p) == nil {
			s.trackCommandApprovalLocked(state.SessionId, p.ToolCall.ID, p.ToolCall.Kind)
		}
	}
}

type commandNotice struct {
	callID, session string
	message         api.SystemMessage
}

func (s *Session) reportApprovedCommands(ctx context.Context) error {
	notice, err := s.prepareCommandNotice(ctx)
	if err != nil || notice == nil {
		return err
	}
	s.mu.Lock()
	tools := s.tools.Clone()
	s.mu.Unlock()
	if tools == nil || tools.DeliverSystem == nil {
		return nil
	}
	receipt, err := tools.DeliverSystem(ctx, notice.message)
	if err != nil || receipt.Deferred {
		return err
	}
	s.mu.Lock()
	same := s.state.Session.SessionId == notice.session
	originalOutcome := s.state.Operations[notice.message.ID].Outcome
	received := false
	if v := s.state.Views[notice.session]; v != nil {
		received = v.CommandResults[notice.callID].Received
	}
	s.mu.Unlock()
	if !same {
		return nil
	}
	if received {
		return s.finishCommandFollowup(notice.callID)
	}
	if originalOutcome != "" {
		return s.recordCommandDelivery(notice.callID, originalOutcome)
	}
	return s.recordCommandDelivery(notice.callID, receipt.Outcome)
}

func (s *Session) prepareCommandNotice(ctx context.Context) (*commandNotice, error) {
	// Native observation holds the adapter gate. Release it before resident
	// admission; the message retains its original conversation fence.
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	if !slices.Contains(s.info.Capabilities, commandObservationCapability) {
		s.mu.Unlock()
		return nil, nil
	}
	ready := s.snapshotLocked().CanSend
	if v := s.state.Views[s.state.Session.SessionId]; v == nil || !v.CommandCaughtUp {
		ready = false
	}
	sid, c := s.state.Session.SessionId, s.client
	var evidence map[string]commandResultEvidence
	if v := s.state.Views[sid]; v != nil {
		evidence = clone(v.CommandResults)
	}
	pending := []string{}
	for id, f := range s.state.CommandFollowups {
		if !f.Done && f.DeliveryFailure == "" {
			pending = append(pending, id)
		}
	}
	s.mu.Unlock()
	if !ready || len(pending) == 0 || c == nil {
		return nil, nil
	}
	// Public, application-scoped observation only. No private session store and
	// no model invocation while a command is still running or awaiting approval.
	var list wire.TaskList
	if err := c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/tasks", nil, &list, "", ""); err != nil {
		return nil, err
	}
	slices.Sort(pending)
	for _, callID := range pending {
		observed := evidence[callID]
		// Require the original canonical result and its ordered turn boundary.
		// An idle state read can overtake SSE; terminal exit alone proves neither.
		if observed.Received {
			if err := s.finishCommandFollowup(callID); err != nil {
				return nil, err
			}
			continue
		}
		if !observed.TurnEnded || observed.Handle == "" {
			continue
		}
		var task *wire.TaskDescriptor
		for i := range list.Tasks {
			t := &list.Tasks[i]
			if t.SessionId == sid && t.Kind == "command" && t.ParentTool != nil && value(t.ParentTool.ToolCallId) == callID && t.Handle == observed.Handle {
				task = t
				break
			}
		}
		if task == nil {
			continue
		} // Absence is not a delivery receipt.

		status := string(task.State)
		if task.Running || !slices.Contains([]wire.TaskState{"completed", "failed", "cancelled", "interrupted", "terminated", "unknown_outcome"}, task.State) {
			if task.State != "running" {
				continue
			}
			// The directory is a committed snapshot. A yielded command may not
			// yet have reconciled its producer's exit. The public terminal read
			// observes that producer without dispatching tools or consuming a
			// model result. Never treat a stale directory's Running as final.
			var output wire.TerminalOutput
			if err := c.json(ctx, "POST", "/sessions/"+idPath(sid)+"/terminals/output", wire.TerminalRequest{SessionId: sid, TerminalId: task.TaskId}, &output, "", ""); err != nil {
				return nil, err
			}
			if output.ExitStatus == nil {
				continue
			}
			status = "waiting for its result after process exit"
		}
		op := "command-result-" + digest([]byte(sid+"\x00"+callID))
		s.mu.Lock()
		j, recorded := s.state.Operations[op]
		s.mu.Unlock()
		if recorded {
			// Unknown dispatch is reconciled by operation reads, never resubmitted.
			if err := s.recordCommandDelivery(callID, j.Outcome); err != nil {
				return nil, err
			}
			continue
		}

		// Stream delivery can advance while the read-only task request runs.
		s.mu.Lock()
		latest := s.state.Views[sid].CommandResults[callID]
		readyNow := s.snapshotLocked().CanSend && s.state.Views[sid].CommandCaughtUp
		s.mu.Unlock()
		if latest.Received {
			return nil, s.finishCommandFollowup(callID)
		}
		if !readyNow {
			return nil, nil
		}
		text := fmt.Sprintf("Command %s is %s.", task.Handle, status)
		return &commandNotice{callID: callID, session: sid, message: api.SystemMessage{
			ID: op, Text: text, Source: api.SystemCommandResult, RuntimeOwner: s.Snapshot().RuntimeOwner,
		}}, nil
	}
	return nil, nil
}

func (s *Session) finishCommandFollowup(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.CommandFollowups[id] = commandFollowup{Done: true}
	return s.saveLocked()
}

// Explicit nondelivery remains a visible failure. Never reuse a rejected ID or
// silently start a retry loop; uncertain effects are resolved only by reads.
func (s *Session) recordCommandDelivery(id, outcome string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.state.CommandFollowups[id]
	switch outcome {
	case "accepted", "committed":
		f.Done = true
		f.DeliveryFailure = ""
	case "rejected", "conflicted":
		f.Done = false
		f.DeliveryFailure = outcome
	default:
		return nil
	}
	s.state.CommandFollowups[id] = f
	err := s.saveLocked()
	s.bumpLocked()
	return err
}

// The exact live lifecycle boundary follows canonical tool results in the feed;
// retain it durably, but do not use a polled idle head as a replacement.
// commandResultEvidence is derived only from model-visible canonical native
// tool results, never from a task directory, terminal stream, prose or approval.
type commandResultEvidence struct {
	TurnID    string `json:"turnID"`
	Handle    string `json:"handle"`
	TurnEnded bool   `json:"turnEnded,omitempty"`
	Received  bool   `json:"received,omitempty"`
}

func commandTerminalState(state string) bool {
	return slices.Contains([]string{"completed", "failed", "cancelled", "interrupted", "terminated", "unknown_outcome"}, state)
}
func observeCommandResult(v *view, e wire.Envelope) {
	if value(e.ParticipantId) != "" || value(e.ApprovalRequestId) != "" || (value(e.Scope) != "" && value(e.Scope) != "main") {
		return
	}
	if e.Lifecycle != nil && e.Kind == "caelis/lifecycle" && commandTerminalState(e.Lifecycle.State) && value(e.TurnId) != "" {
		for id, f := range v.CommandResults {
			if f.TurnID == value(e.TurnId) {
				f.TurnEnded = true
				v.CommandResults[id] = f
			}
		}
		return
	}
	if e.Delivery.Mode != "canonical" || e.Kind != "session/update" {
		return
	}
	var u struct {
		Kind   string `json:"sessionUpdate"`
		Name   string `json:"name"`
		CallID string `json:"toolCallId"`
		Input  struct {
			Action string `json:"action"`
		} `json:"rawInput"`
		Output struct {
			Handle string `json:"handle"`
			State  string `json:"state"`
			Tasks  []struct {
				Handle string `json:"handle"`
				State  string `json:"state"`
			} `json:"tasks"`
		} `json:"rawOutput"`
	}
	if json.Unmarshal(value(e.Update), &u) != nil || u.Kind != "tool_call_update" {
		return
	}
	if u.Name == "RunCommand" && u.CallID != "" && u.Output.Handle != "" && u.Output.State != "" && value(e.TurnId) != "" {
		if v.CommandResults == nil {
			v.CommandResults = map[string]commandResultEvidence{}
		}
		old := v.CommandResults[u.CallID]
		v.CommandResults[u.CallID] = commandResultEvidence{TurnID: value(e.TurnId), Handle: u.Output.Handle, TurnEnded: old.TurnEnded, Received: old.Received || commandTerminalState(u.Output.State)}
	}
	if u.Name == "Task" && (u.Input.Action == "read" || u.Input.Action == "wait") {
		mark := func(handle, state string) {
			if handle == "" || !commandTerminalState(state) {
				return
			}
			for id, f := range v.CommandResults {
				if f.Handle == handle {
					f.Received = true
					v.CommandResults[id] = f
				}
			}
		}
		mark(u.Output.Handle, u.Output.State)
		for _, task := range u.Output.Tasks {
			mark(task.Handle, task.State)
		}
	}
}
