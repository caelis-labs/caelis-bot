package caelis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
)

func (s *Session) Snapshot() api.Snapshot {
	if s.mu.TryLock() {
		out := s.snapshotLocked()
		s.cached.Store(&out)
		s.mu.Unlock()
		return out
	}
	if snapshot := s.cached.Load(); snapshot != nil {
		out := clone(*snapshot)
		out.RuntimeOwner = snapshot.RuntimeOwner
		out.InputSources = append([]api.InputSource(nil), snapshot.InputSources...)
		out.ScheduledSources = append([]api.ScheduledSource(nil), snapshot.ScheduledSources...)
		out.ReportSources = append([]api.ReportSource(nil), snapshot.ReportSources...)
		for i := range out.Approvals {
			out.Approvals[i].TaskID = snapshot.Approvals[i].TaskID
			out.Approvals[i].NativeID = snapshot.Approvals[i].NativeID
		}
		return out
	}
	return api.Snapshot{Connection: "connecting", Phase: "idle", Items: []api.Item{}, Approvals: []api.Approval{}}
}
func (s *Session) Revision() uint64 {
	if v := s.cached.Load(); v != nil {
		return v.Revision
	}
	return 0
}

// Idle state snapshots may omit the active target. Transcript TurnKey values
// come from canonical envelopes; retain that identity for presentation and
// completion hooks only, never to authorize a cancel or approval.
func observedTurn(v *view) string {
	if v == nil {
		return ""
	}
	if turn := value(v.State.Run.TurnId); turn != "" {
		return turn
	}
	if v.HeadTarget.TurnId != "" {
		return v.HeadTarget.TurnId
	}
	for i := len(v.Items) - 1; i >= 0; i-- {
		if v.Items[i].TurnKey != "" {
			return v.Items[i].TurnKey
		}
	}
	return ""
}

// Replacement history omits live transport IDs. Retain missing fields only
// when its canonical turn and any explicit target fields agree with the saved
// observation. This evidence is for reports, never a new cancel target.
func retainHeadTarget(next, prior wire.TurnTarget) wire.TurnTarget {
	if next.TurnId == "" || next.TurnId != prior.TurnId ||
		next.HandleId != "" && next.HandleId != prior.HandleId ||
		next.RunId != "" && next.RunId != prior.RunId {
		return next
	}
	if next.HandleId == "" {
		next.HandleId = prior.HandleId
	}
	if next.RunId == "" {
		next.RunId = prior.RunId
	}
	return next
}

func observeStateHead(v *view, run wire.RunState) {
	if value(run.TurnId) != "" {
		v.HeadTarget = retainHeadTarget(wire.TurnTarget{TurnId: value(run.TurnId), HandleId: value(run.HandleId), RunId: value(run.RunId)}, v.HeadTarget)
	}
}

func retireRunHead(v *view, next string) {
	prior := observedTurn(v)
	if prior == "" || next == "" || prior == next {
		return
	}
	if v.RetiredTurns == nil {
		v.RetiredTurns = map[string]bool{}
	}
	v.RetiredTurns[prior] = true
}

func retiredRunHead(v *view, turn string) bool {
	return turn != "" && turn != value(v.State.Run.TurnId) && v.RetiredTurns[turn]
}

// Only a new running lifecycle can advance the projected head. Older terminal
// facts remain on their own turn; they cannot complete an unresolved successor.
func acceptsRunHead(v *view, e wire.Envelope) bool {
	turn, current := value(e.TurnId), value(v.State.Run.TurnId)
	if turn != "" && v.RetiredTurns[turn] {
		return false
	}
	if turn == "" || current == "" || turn == current {
		return true
	}
	if e.Kind == "caelis/lifecycle" && e.Lifecycle != nil && (e.Lifecycle.State == "running" || e.Lifecycle.State == "started") {
		retireRunHead(v, turn)
		return true
	}
	return false
}
func (s *Session) WaitSnapshot(ctx context.Context, rev uint64) (api.Snapshot, error) {
	for {
		s.mu.Lock()
		if s.revision > rev {
			v := s.snapshotLocked()
			s.mu.Unlock()
			return v, nil
		}
		ch := s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return api.Snapshot{}, ctx.Err()
		case <-ch:
		}
	}
}
func (s *Session) snapshotLocked() api.Snapshot {
	out := api.Snapshot{Revision: s.revision, Connection: "offline", ConnectionIssue: "connection_lost", Phase: "disconnected", Message: s.issue, Items: []api.Item{}, Approvals: []api.Approval{}, Reviews: []api.Review{}, References: []api.Reference{}, LastReceipt: s.state.LastReceipt}
	out.RuntimeOwner = (inputScope{InstanceID: s.state.InstanceID, SessionID: s.state.Session.SessionId}).owner()
	if s.connected && !s.closed {
		out.Connection = "ready"
		out.ConnectionIssue = ""
		out.Phase = "idle"
	}
	v := s.state.Views[s.state.Session.SessionId]
	_, history := s.earlierViewLocked()
	out.HasEarlier = history != nil
	if v != nil {
		out.Items = clone(v.Items)
		s.correlateSessionInputs(&out, s.state.Session.SessionId)
		s.appendInputSources(&out, v, s.state.Session.SessionId)
		out.CurrentTurn = observedTurn(v)
		if s.connected && !value(v.State.Run.Active) {
			switch value(v.State.Run.Status) {
			case "completed", "failed", "interrupted", "unknown":
				out.Phase = value(v.State.Run.Status)
			case "cancelled":
				out.Phase = "interrupted"
			}
		}
		if s.connected && value(v.State.Run.Active) {
			out.Phase = "working"
			out.CanInterrupt = true
		}
		if out.Phase == "failed" && out.Message == "" {
			out.Message = "本次请求未完成，请重试。"
			if v.Failure != "" {
				out.Message = "本次请求未完成：" + v.Failure
			}
		}
	}
	if s.connected {
		for sid, v := range s.state.Views {
			a := v.State.Approval.Active
			if a == nil || s.automaticApprovalLocked(sid, a) {
				continue
			}
			raw, _ := json.Marshal(a.Permission)
			var p nativePermission
			_ = json.Unmarshal(raw, &p)
			item := approvalDisplay(a, p)
			item.ID, item.NativeID = approvalID(s.state.InstanceID, sid, a), a.RequestId
			item.NoticeKey = "chat.caelisApproval"
			if sid != "" && sid == s.state.Session.SessionId {
				item.Owner = "conversation"
			} else if sid != "" {
				item.NoticeKey = "chat.workerApproval"
				for _, worker := range s.state.Workers {
					if worker.Binding.SessionId == sid {
						item.Owner = "task"
						item.TaskID = worker.Task.ID
						break
					}
				}
			}
			for _, o := range p.Options {
				item.Choices = append(item.Choices, api.Choice{ID: o.ID, Label: o.Name, Scope: o.Kind})
			}
			out.Approvals = append(out.Approvals, item)
		}
	}
	for sid, view := range s.state.Views {
		for _, review := range view.Reviews {
			out.Reviews = append(out.Reviews, s.ownedReview(sid, review.Review))
		}
		for _, review := range view.LiveReviews {
			out.Reviews = append(out.Reviews, s.ownedReview(sid, review.Review))
		}
	}
	slices.SortFunc(out.Reviews, func(a, b api.Review) int { return strings.Compare(a.ID, b.ID) })
	for id, f := range s.state.CommandFollowups {
		if f.DeliveryFailure != "" && !f.Done {
			if v != nil && v.CommandResults[id].Received {
				continue
			}
			if out.Message == "" && s.connected {
				out.Message = "命令已结束，但结果跟进未被接受。请让 Bot 检查保留的命令结果；不会自动重跑命令。"
			}
		}
	}
	slices.SortFunc(out.Approvals, func(a, b api.Approval) int { return strings.Compare(a.ID, b.ID) })
	for _, approval := range out.Approvals {
		if approval.Owner != "task" {
			out.Phase = "waiting_approval"
			break
		}
	}
	unknown := v != nil && value(v.State.Run.Status) == "unknown"
	dispatching := false
	if s.pluginConfigurationUnconfirmedLocked() {
		unknown = true
	}
	for id, j := range s.state.Operations {
		if j.Outcome == "unknown" && id != s.sendingScheduled && !s.workerOperationLocked(j) && !j.deferredRenewal(id) {
			if s.dispatching[id] {
				dispatching = true
			} else {
				unknown = true
			}
		}
	}
	if dispatching && !unknown && s.connected && out.Phase != "waiting_approval" {
		// Journaling an in-flight effect as unknown is crash safety, not a
		// product failure. Keep admission fenced while showing normal progress.
		out.Phase = "sending"
	}
	if unknown && s.connected {
		out.Phase = "unknown"
	}
	terminalReceipt := s.terminalReceiptPendingLocked()
	out.CanSend = !terminalReceipt && s.connected && !s.closed && !unknown && !dispatching && v != nil && !value(v.State.Run.Active) && v.State.Approval.Active == nil
	// Worker approval does not turn the secretary's input into a steer action.
	out.CanSteer = !terminalReceipt && s.connected && !s.closed && !unknown && !dispatching && v != nil && value(v.State.Run.Active) && value(v.State.Run.TurnId) != "" && value(v.State.Run.HandleId) != "" && value(v.State.Run.RunId) != ""
	if v != nil {
		var past []api.Item
		for _, sid := range s.state.PastSessions {
			if old := s.state.Views[sid]; old != nil {
				history := api.Snapshot{Items: clone(old.Items)}
				s.correlateSessionInputs(&history, sid)
				s.appendInputSources(&history, old, sid)
				out.InputSources = append(out.InputSources, history.InputSources...)
				past = append(past, history.Items...)
			}
		}
		out.Items = append(past, out.Items...)
		return s.presentScheduled(out, v)
	}
	return out
}

// Initial prompts identify their turn in the command receipt; only steered
// inputs carry input_operation_id on their canonical message envelopes.
func (s *Session) correlateUserInputs(out *api.Snapshot) {
	s.correlateSessionInputs(out, s.state.Session.SessionId)
}
func (s *Session) correlateSessionInputs(out *api.Snapshot, sid string) {
	path := "/sessions/" + idPath(sid) + "/prompt"
	for id, j := range s.state.Operations {
		if j.Path != path || j.TurnID == "" || !succeeded(wire.Outcome(j.Outcome)) {
			continue
		}
		for i := range out.Items {
			item := &out.Items[i]
			if (item.Kind == "user" || item.Kind == "hostNotice") && item.RequestID == "" && item.TurnKey == j.TurnID {
				item.RequestID = id
			}
		}
	}
	for _, j := range s.state.Operations {
		if j.Path == path && j.Outcome == "unknown" && j.PendingInput != nil {
			out.Items = slices.DeleteFunc(out.Items, func(item api.Item) bool {
				return item.Kind == "user" && item.RequestID == "" && !slices.Contains(j.PendingInput.VisibleIDs, item.ID)
			})
		}
	}
	for index := range out.Items {
		item := &out.Items[index]
		if item.Kind == "user" || item.Kind == "hostNotice" {
			if n := s.state.ContextInputs[item.RequestID]; n > 0 {
				if len(item.Text) >= n {
					item.Text = item.Text[n:]
				} else {
					item.Text = ""
				}
			}
		}
	}
}

type approvalRef struct {
	sid    string
	active *wire.ActiveApproval
}

func approvalID(instance, sid string, a *wire.ActiveApproval) string {
	b, _ := json.Marshal(a)
	h := sha256.Sum256(append([]byte(instance+"\x00"+sid+"\x00"), b...))
	return hex.EncodeToString(h[:])
}
func (s *Session) approvalLocked(id string) (approvalRef, bool) {
	for sid, v := range s.state.Views {
		if a := v.State.Approval.Active; a != nil && !s.automaticApprovalLocked(sid, a) && approvalID(s.state.InstanceID, sid, a) == id {
			return approvalRef{sid, clone(a)}, true
		}
	}
	return approvalRef{}, false
}

// Unscoped projection is display-only. Native feed paths use applyScopedEnvelope.
func applyEnvelope(v *view, e wire.Envelope, scheduled ...bool) {
	applyScopedEnvelope(v, e, inputScope{}, scheduled...)
}

func applyScopedEnvelope(v *view, e wire.Envelope, scope inputScope, scheduled ...bool) {
	key := value(e.ProjectionId)
	if key == "" {
		key = value(e.EventId)
	}
	if key != "" {
		if v.Seen[key] {
			return
		}
		v.Seen[key] = true
	}
	if v.WorkerOutput != nil && v.WorkerOutput.Session == scope.SessionID {
		if v.WorkerOutput.apply(e) == nil {
			v.WorkerOutput.bound()
		}
	}
	if e.Kind == "caelis/approval_review" {
		applyReview(v, e)
		return
	}
	observeCommandResult(v, e)
	// ACP permission events are notifications, not the durable Control approval
	// shape. Reconcile the exact head before presenting native targets/choices.
	if e.Kind == "session/request_permission" || value(e.ApprovalRequestId) != "" {
		v.ApprovalDirty = true
		v.ApprovalVersion++
	}
	isMain := (value(e.Scope) == "" || value(e.Scope) == "main") && value(e.ApprovalRequestId) == ""
	if isMain && e.ParentTool != nil {
		// A child observation cannot replace the foreground turn or hint.
		// Its exact parent invocation may still receive a terminal update.
		applyToolActivity(v, e)
		return
	}
	head := isMain && acceptsRunHead(v, e)
	if e.Kind == "caelis/error" && head {
		v.Failure = value(e.Error)
	}
	if e.Lifecycle != nil && e.Kind == "caelis/lifecycle" && head {
		switch e.Lifecycle.State {
		case "running", "started":
			v.State.Run.Active = pointer(true)
			v.Failure = ""
		case "completed", "failed", "cancelled", "interrupted", "stopped":
			finishAssistantStream(v, value(e.TurnId))
			v.State.Run.Active = pointer(false)
			v.ApprovalDirty = true
			v.ApprovalVersion++
			if e.Lifecycle.State == "failed" && v.Failure == "" {
				v.Failure = value(e.Lifecycle.Reason)
			}
		}
		v.State.Run.Status = &e.Lifecycle.State
	}
	if e.Lifecycle != nil && e.Kind == "caelis/lifecycle" && isMain && !head {
		finishAssistantStream(v, value(e.TurnId))
	}
	if e.TurnId != nil && head {
		if v.HeadTarget.TurnId != *e.TurnId {
			v.HeadTarget = wire.TurnTarget{TurnId: *e.TurnId}
		}
		if e.HandleId != nil {
			v.HeadTarget.HandleId = *e.HandleId
		}
		if e.RunId != nil {
			v.HeadTarget.RunId = *e.RunId
		}
		v.State.Run.TurnId = e.TurnId
	}
	if e.HandleId != nil && head {
		v.State.Run.HandleId = e.HandleId
	}
	if e.RunId != nil && head {
		v.State.Run.RunId = e.RunId
	}
	if !isMain || len(value(e.Update)) == 0 {
		return
	}
	var update struct {
		Kind       string          `json:"sessionUpdate"`
		Content    json.RawMessage `json:"content"`
		MessageID  string          `json:"messageId"`
		Name       string          `json:"name"`
		ToolCallID string          `json:"toolCallId"`
		Status     string          `json:"status"`
	}
	if json.Unmarshal(value(e.Update), &update) != nil {
		return
	}
	if update.Kind == "tool_call_update" && update.Name == "PublishArtifact" && update.Status == "completed" {
		var tool struct {
			RawOutput json.RawMessage `json:"rawOutput"`
		}
		_ = json.Unmarshal(value(e.Update), &tool)
		var output struct {
			Resource wire.SessionResource `json:"resource"`
		}
		// Native resource tools return a text result. Control projects that
		// JSON text under rawOutput.result; message content is a different
		// union member from the tool's content array.
		var raw struct {
			Result string `json:"result"`
		}
		_ = json.Unmarshal(tool.RawOutput, &raw)
		if json.Unmarshal([]byte(raw.Result), &output) == nil && output.Resource.Id != "" && output.Resource.SessionId == v.State.SessionId {
			resource := output.Resource
			id := "resource:" + v.State.SessionId + ":" + resource.Id
			found := false
			for _, item := range v.Items {
				if item.ID == id {
					found = true
				}
			}
			if !found {
				name := filepath.Base(strings.ReplaceAll(resource.Name, "\\", "/"))
				if name == "." || name == ".." || name == "" || name == string(filepath.Separator) {
					name = "artifact"
				}
				v.Items = append(v.Items, api.Item{ID: id, TurnKey: value(e.TurnId), Kind: "tool", Text: "文件已生成", Status: "completed", Artifacts: []api.Artifact{{ID: id, Name: name}}})
			}
		}
	}
	if update.Kind == "tool_call" || update.Kind == "tool_call_update" {
		// A new foreground tool is a text-segment boundary. Sparse updates
		// and child completions can arrive during a reply and must not end it.
		if update.Kind == "tool_call" {
			finishAssistantStream(v, value(e.TurnId))
		}
		applyToolActivity(v, e)
		return
	}
	if update.Kind == "agent_thought_chunk" {
		finishAssistantStream(v, value(e.TurnId))
		return
	}
	kind := ""
	switch update.Kind {
	case "user_message_chunk":
		// Control reports are agent-communication context, not human messages.
		// The model's eventual assistant reply is still presented normally.
		if e.AgentCommunicationSource != nil {
			return
		}
		kind = "user"
		if len(scheduled) > 0 && scheduled[0] {
			kind = "activation"
		}
		if len(scheduled) > 1 && scheduled[1] {
			kind = "hostNotice"
		}
	case "agent_message_chunk":
		kind = "assistant"
		if value(e.Final) {
			finishAssistantStream(v, value(e.TurnId))
		}
	default:
		return
	}
	var content struct{ Type, Text string }
	if json.Unmarshal(update.Content, &content) != nil || content.Type != "text" || content.Text == "" {
		return
	}
	turn := value(e.TurnId)
	if turn == "" {
		turn = value(e.ActivityId)
	}
	id := turn + "/" + kind
	requestID := ""
	if kind == "user" || kind == "activation" || kind == "hostNotice" {
		requestID = value(e.InputOperationId)
		if requestID != "" {
			id += "/" + requestID
		}
	}
	if update.MessageID != "" {
		id += "/" + update.MessageID
	}
	if kind == "user" && value(e.TurnId) != "" && scope.owner() != "" && scope.SessionID == v.State.SessionId && (e.SessionId == nil || value(e.SessionId) == scope.SessionID) {
		scope.TurnID = value(e.TurnId)
		if v.InputScopes == nil {
			v.InputScopes = map[string]inputScope{}
		}
		if old, exists := v.InputScopes[id]; exists {
			scope = old // replay cannot replace original evidence or its absence
		} else if slices.ContainsFunc(v.Items, func(item api.Item) bool { return item.ID == id }) {
			scope = inputScope{} // another chunk cannot upgrade unproven history
		}
		v.InputScopes[id] = scope
	}
	status := "completed"
	if kind == "assistant" {
		// ACP chunks carry streaming text; an explicit final frame or the
		// next foreground segment/lifecycle closes it. Never infer from prose.
		finishAssistantStream(v, turn)
		if !value(e.Final) {
			status = "inProgress"
		}
	}
	for i := len(v.Items) - 1; i >= 0; i-- {
		if v.Items[i].ID == id {
			v.Items[i].Text += content.Text
			v.Items[i].Status = status
			return
		}
	}
	v.Items = append(v.Items, api.Item{ID: id, RequestID: requestID, TurnKey: turn, Kind: kind, Text: content.Text, Status: status})
}

func finishAssistantStream(v *view, turn string) {
	if turn == "" {
		turn = value(v.State.Run.TurnId)
	}
	if turn == "" {
		return
	}
	for i := range v.Items {
		item := &v.Items[i]
		if item.Kind == "assistant" && item.TurnKey == turn && item.Status == "inProgress" {
			item.Status = "completed"
		}
	}
}
func (s *Session) ensureStreamLocked(sid string) {
	if s.streams[sid] || s.ctx == nil || s.closed {
		return
	}
	s.streams[sid] = true
	if v := s.state.Views[sid]; v != nil {
		v.CommandCaughtUp = false
	}
	if s.streamCtx == nil {
		s.streamCtx, s.streamCancel = context.WithCancel(s.ctx)
	}
	ctx := s.streamCtx
	generation := s.generation
	instance := s.state.InstanceID
	c := s.client
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			if s.generation == generation {
				s.streams[sid] = false
			}
			s.mu.Unlock()
		}()
		for attempt := 0; ctx.Err() == nil && attempt < 3; attempt++ {
			err := s.watch(ctx, c, sid, instance)
			if ctx.Err() != nil {
				return
			}
			s.mu.Lock()
			if s.generation == generation && s.client == c {
				s.observeWorkerActivityLocked(sid, wire.SessionState{})
			}
			s.mu.Unlock()
			s.componentError("session_observation", sid, err)
			// A retry against a dead endpoint cannot discover its replacement.
			// Read discovery only after stream failure; healthy idle observers do
			// no periodic Host polling. The normal connect owner authenticates and
			// checks Store/principal before accepting the new instance.
			if next, _, discoveryErr := Discover(s.settings); discoveryErr == nil && next.InstanceID != instance {
				s.mu.Lock()
				if s.generation == generation && !s.closed {
					s.connected = false
					s.bumpLocked()
					s.requestRefreshLocked("connection")
				}
				s.mu.Unlock()
				return
			}
			timer := time.NewTimer(time.Second * time.Duration(1<<min(attempt, 4)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}

	}()
}
func (s *Session) watch(ctx context.Context, c *client, sid, instance string) error {
	s.mu.Lock()
	old := s.state.Views[sid]
	cursor := ""
	if old != nil {
		cursor = old.Cursor
	}
	s.mu.Unlock()
	path := "/sessions/" + idPath(sid) + "/reconnect?history_turns=" + "1"
	if cursor != "" {
		path += "&after=" + url.QueryEscape(cursor)
	}
	var bootstrap *wire.SessionState
	var staged *view
	var snapshotID string
	var page int
	return c.stream(ctx, path, "", func(f frame) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed || ctx.Err() != nil || s.state.InstanceID != instance {
			return errors.New("旧 Host 观察已失效")
		}
		if bootstrap == nil {
			if f.event != "caelis.control.bootstrap" {
				return errors.New("缺少原子恢复状态")
			}
			var st wire.SessionState
			if json.Unmarshal(f.data, &st) != nil || st.SessionId != sid || st.ProtocolVersion != 1 || st.ApiVersion != "v1" || st.EnvelopeVersion != "caelis.control.envelope/v1" {
				return errors.New("恢复状态不匹配")
			}
			bootstrap = &st
			v := s.state.Views[sid]
			if v == nil {
				v = &view{Items: []api.Item{}, Seen: map[string]bool{}}
				s.state.Views[sid] = v
			}
			if retiredRunHead(v, value(st.Run.TurnId)) {
				return errors.New("恢复状态引用已被后续任务替代的 Turn")
			}
			s.retainTerminalCallTurns(v, v, sid)
			retireRunHead(v, value(st.Run.TurnId))
			observeStateHead(v, st.Run)
			v.State = st
			s.observeWorkerActivityLocked(sid, st)
			v.Usage, v.UsageTurn, v.ModelTurn = api.ContextUsage{}, "", ""
			v.CommandCaughtUp = false
			s.observeApprovalHeadLocked(st)
			v.ApprovalDirty = false
			v.ApprovalVersion++
			v.Observed++
			s.bumpLocked()
			return nil
		}
		if f.event == "caelis.control.done" {
			return errors.New("观察结束")
		}
		if f.event != "caelis.control.delivery" {
			// Unlike optional Codex notifications, this is a canonical stream;
			// unknown delivery semantics cannot safely advance its recovery cursor.
			s.diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "caelis", Code: "unexpected_feed_event", Method: f.event, Thread: sid,
				Reason: "canonical stream requires recovery; no cursor advanced", Fingerprint: diagnosticlog.Fingerprint(f.data), Bytes: len(f.data)})
			return errors.New("Caelis 观察需要重新恢复")
		}
		var d wire.SessionFeedDelivery
		if json.Unmarshal(f.data, &d) != nil || f.id != "" && f.id != value(d.NextCursor) {
			return errors.New("状态游标不匹配")
		}
		v := s.state.Views[sid]
		switch d.Kind {
		case "replace_begin":
			if staged != nil || value(d.SnapshotId) == "" || value(d.Page) != 0 || d.Source != "replacement" || len(d.Events) != 0 || value(d.NextCursor) != "" {
				return errors.New("重复或无效状态替换")
			}
			v.CommandCaughtUp = false
			snapshotID = value(d.SnapshotId)
			page = 0
			staged = &view{State: *bootstrap, Items: []api.Item{}, Seen: map[string]bool{}}
			// A canonical replacement may replay older Turns before the current
			// bootstrap Turn. Start its temporary head from history, not B.
			staged.State.Run = wire.RunState{}
			return nil
		case "replace_page":
			if staged == nil || snapshotID != value(d.SnapshotId) || value(d.Page) != page || d.Source != "replacement" || value(d.NextCursor) != "" {
				return errors.New("恢复页面不连续")
			}
			page++
			for _, e := range d.Events {
				s.logEnvelope(e)
				s.applyScheduledEnvelope(staged, e, inputScope{InstanceID: instance, SessionID: sid})
			}
			return nil
		case "replace_end":
			if staged == nil || snapshotID != value(d.SnapshotId) || value(d.Page) != page || d.Source != "replacement" || len(d.Events) != 0 || value(d.NextCursor) != "" {
				return errors.New("恢复快照不匹配")
			}
			for id, f := range staged.CommandResults {
				if old := v.CommandResults[id]; old.TurnID == f.TurnID && old.Handle == f.Handle {
					f.TurnEnded = f.TurnEnded || old.TurnEnded
					f.Received = f.Received || old.Received
					staged.CommandResults[id] = f
				}
			}
			// Display history is bounded; original command followups still own
			// their durable evidence, including results outside this window.
			for id, f := range v.CommandResults {
				if _, exists := staged.CommandResults[id]; !exists {
					if staged.CommandResults == nil {
						staged.CommandResults = map[string]commandResultEvidence{}
					}
					staged.CommandResults[id] = f
				}
			}
			retireRunHead(v, value(bootstrap.Run.TurnId))
			if len(v.RetiredTurns) > 0 {
				if staged.RetiredTurns == nil {
					staged.RetiredTurns = map[string]bool{}
				}
				for turn := range v.RetiredTurns {
					staged.RetiredTurns[turn] = true
				}
			}
			// The bootstrap is the authoritative live head. Historical replay
			// must never retire it, even if replacement omitted its lifecycle.
			delete(staged.RetiredTurns, value(bootstrap.Run.TurnId))
			// A canonical replacement cannot relabel retained inputs, including
			// legacy inputs with no original scope. A new Host's replay proves
			// its serving scope, not the origin of an already retained input.
			// Keep this evidence beyond the bounded display window: LoadEarlier
			// may later bring the same item back after a Host replacement.
			if staged.InputScopes == nil {
				staged.InputScopes = map[string]inputScope{}
			}
			for id, scope := range v.InputScopes {
				staged.InputScopes[id] = scope
			}
			for _, item := range v.Items {
				if item.Kind == "user" {
					staged.InputScopes[item.ID] = v.InputScopes[item.ID]
				}
			}
			if v.WorkerOutput != nil && v.WorkerOutput.Session == sid {
				if staged.WorkerOutput == nil {
					staged.WorkerOutput = newWorkerTranscript(sid)
				}
				staged.WorkerOutput.retainMissing(v.WorkerOutput)
			}
			s.retainTerminalCallTurns(staged, v, sid)
			staged.HeadTarget = retainHeadTarget(staged.HeadTarget, v.HeadTarget)
			observeStateHead(staged, bootstrap.Run)
			staged.State = *bootstrap
			staged.ApprovalDirty = false
			v = staged
			s.state.Views[sid] = v
			staged = nil
		case "append_page":
			if staged != nil {
				return errors.New("替换未完成")
			}
			for _, e := range d.Events {
				s.logEnvelope(e)
				if d.Source == wire.StreamSourceClassExact && v.CommandCaughtUp {
					s.observeContinuity(v, e, time.Now().Round(0))
					applyLiveUsage(v, e, time.Now().Round(0))
				}
				s.applyScheduledEnvelope(v, e, inputScope{InstanceID: instance, SessionID: sid})
				s.observeCommandApprovalLocked(sid, e)
			}
		case "sync", "status":
			if staged != nil {
				return errors.New("替换未完成")
			}
		default:
			return fmt.Errorf("未知状态投影 %s", d.Kind)
		}
		if d.Kind == "sync" {
			v.CommandCaughtUp = true
			if cursor == "" || snapshotID != "" || d.HistoryBefore != nil {
				v.HistoryBefore = value(d.HistoryBefore)
			}
		}
		v.Observed++
		delete(s.observationBudgets, s.sessionObservationKeyLocked(sid))
		s.observeWorkerActivityLocked(sid, v.State)
		if d.NextCursor != nil {
			v.Cursor = *d.NextCursor
		}
		if e := s.publishProjectionLocked(d, time.Now()); e != nil {
			return e
		}
		if v.ApprovalDirty {
			s.requestRefreshLocked()
		}
		return nil
	})
}

// Only reconstructible, non-final prose may be checkpointed in batches. Native
// commands, callback receipts, approvals, canonical events and stream boundaries
// retain their immediate durable writes. The saved cursor and its projection
// are always written together; a crash replays from that last checkpoint.
func (s *Session) publishProjectionLocked(d wire.SessionFeedDelivery, now time.Time) error {
	soft := transientProse(d)
	if !soft || now.Sub(s.projectionSavedAt) >= time.Second {
		if err := s.saveLocked(); err != nil {
			return err
		}
		s.projectionSavedAt = now
	}
	if !soft || now.Sub(s.projectionPublishedAt) >= 50*time.Millisecond {
		s.bumpLocked()
		s.projectionPublishedAt = now
	}
	return nil
}

func transientProse(d wire.SessionFeedDelivery) bool {
	if d.Kind != "append_page" || len(d.Events) == 0 {
		return false
	}
	for _, e := range d.Events {
		if e.Delivery.Mode != wire.DeliveryModeTransient || e.Kind != "session/update" || value(e.Final) ||
			e.Permission != nil || e.ApprovalReview != nil || value(e.ApprovalRequestId) != "" || e.Lifecycle != nil {
			return false
		}
		var update struct {
			Kind string `json:"sessionUpdate"`
		}
		if json.Unmarshal(value(e.Update), &update) != nil || (update.Kind != "agent_message_chunk" && update.Kind != "agent_thought_chunk") {
			return false
		}
	}
	return true
}

func (s *Session) pollLoop(ctx context.Context) {
	defer s.wg.Done()
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		var e error
		work, cancel := context.WithTimeout(ctx, 15*time.Second)
		if e = lockwait.Lock(work, &s.step); e != nil {
			cancel()
			continue
		}
		s.mu.Lock()
		selected := s.takeObservationsLocked(time.Now())
		s.mu.Unlock()
		if len(selected) > 0 {
			e = s.refreshSelected(work, selected)
		}
		s.step.Unlock()
		cancel()
		if e == nil && !s.retainedWorkers {
			if err := s.reportApprovedCommands(ctx); err != nil {
				s.componentError("command_followup", "", err)
			}
		}
		if e != nil && ctx.Err() == nil {
			_ = s.fail(e)
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-s.wake:
		}
	}
}
func (s *Session) refresh(ctx context.Context) error {
	return s.refreshSelected(ctx, nil)
}

func (s *Session) refreshSelected(ctx context.Context, selected observationPlan) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	c := s.client
	connected := s.connected
	sid := s.state.Session.SessionId
	s.mu.Unlock()
	if c == nil || !connected {
		if e := s.reloadOriginal(); e != nil {
			return e
		}
		if e := s.connectWithRecovery(ctx); e != nil {
			return e
		}
		s.mu.Lock()
		c = s.client
		sid = s.state.Session.SessionId
		s.connected = true
		s.issue = ""
		selected = s.takeObservationsLocked(time.Now())
		s.mu.Unlock()
	}
	info, e := initialize(ctx, c)
	if e != nil {
		return e
	}
	s.mu.Lock()
	same := value(info.InstanceId) == s.state.InstanceID && value(info.StoreId) == s.state.StoreID
	s.mu.Unlock()
	if !same {
		return errors.New("Caelis Host 已变化，正在核对绑定")
	}
	if !s.retainedWorkers && selected.includes("main") {
		if err := s.refreshMain(ctx, c, sid); err != nil {
			s.componentError("session_state", sid, err)
		}
	}
	if err := s.refreshSelectedWorkers(ctx, c, selected); err != nil {
		s.componentError("worker_state", "", err)
	}
	if !s.retainedWorkers && selected.includes("configuration") {
		if err := s.maintainConfigurations(ctx); err != nil {
			s.componentError("configuration", "", err)
		}
	}
	if err := s.recoverSelectedOperations(ctx, selected); err != nil {
		s.componentError("operation_receipts", "", err)
	}
	return nil
}

func (s *Session) refreshMain(ctx context.Context, c *client, sid string) error {
	s.mu.Lock()
	before := s.state.Views[sid]
	var observed, approvalVersion uint64
	if before != nil {
		observed = before.Observed
		approvalVersion = before.ApprovalVersion
	}
	s.mu.Unlock()
	var state wire.SessionState
	if e := c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/state", nil, &state, "", ""); e != nil {
		return e
	}
	if state.SessionId != sid {
		return errors.New("Caelis 对话状态不匹配")
	}
	s.mu.Lock()
	v := s.state.Views[sid]
	if v == nil {
		v = &view{Items: []api.Item{}, Seen: map[string]bool{}}
		s.state.Views[sid] = v
	}
	if v == before && v.Observed == observed || before == nil && v.Observed == 0 {
		if retiredRunHead(v, value(state.Run.TurnId)) {
			s.mu.Unlock()
			return nil // A stale state read cannot replace the retained head.
		}
		approval := v.State.Approval
		retireRunHead(v, value(state.Run.TurnId))
		observeStateHead(v, state.Run)
		v.State = state
		v.State.Approval = approval
	}
	reconcileApproval(v, before, approvalVersion, state)
	s.observeApprovalHeadLocked(v.State)
	s.ensureStreamLocked(sid)
	turn := observedTurn(v)
	finished := turn != "" && !value(v.State.Run.Active) && slices.Contains([]string{"completed", "failed", "interrupted", "cancelled"}, value(v.State.Run.Status)) && s.state.FinishedTurn != turn
	if finished {
		s.state.FinishedTurn = turn
	}
	e := s.saveLocked()
	s.bumpLocked()
	s.mu.Unlock()
	if e != nil {
		return e
	}
	if finished && s.tools != nil && s.tools.FinishTurn != nil {
		s.tools.FinishTurn()
	}
	return nil
}

func (s *Session) recoverOperations(ctx context.Context) error {
	return s.recoverSelectedOperations(ctx, nil)
}

func (s *Session) recoverSelectedOperations(ctx context.Context, selected observationPlan) error {
	s.mu.Lock()
	ops := clone(s.state.Operations)
	s.mu.Unlock()
	var failures error
	for id, j := range ops {
		if j.Outcome != "unknown" || !selected.includes("operation:"+id) {
			continue
		}
		failures = errors.Join(failures, s.recoverOperation(ctx, id))
	}
	return failures
}

// Read only the original operation. A missing operation is still unknown: a
// dispatching POST may have reached the server after the read began.
func (s *Session) recoverOperation(ctx context.Context, id string) error {
	s.mu.Lock()
	j, exists := s.state.Operations[id]
	// Principal-global legacy IDs cannot prove another source's receipt belongs
	// to this foreign binding. Known outcomes stay readable; local/helper IDs and
	// properly scoped foreign recovery are unchanged.
	if exists && j.Outcome == "unknown" && s.workSourceIdentity != "" {
		if j.Work == nil {
			s.mu.Unlock()
			return nil
		}
		w, owned := s.state.Workers[j.Work.TaskID]
		if !owned || w.NativeScope == "" {
			s.mu.Unlock()
			return nil
		}
	}
	c, generation := s.client, s.generation
	s.mu.Unlock()
	if !exists || j.Outcome != "unknown" {
		return nil
	}
	if c == nil {
		return errors.New("original operation owner unavailable")
	}
	receiptID := id
	if j.ReceiptID != "" {
		receiptID = j.ReceiptID
	}
	var op wire.CommandResult
	if err := c.json(ctx, "GET", "/operations/"+idPath(receiptID), nil, &op, "", ""); err != nil {
		if isRemoteStatus(err, 404) {
			return nil
		}
		return err
	}
	if op.OperationId != receiptID {
		return errors.New("Caelis 操作恢复回执不匹配")
	}
	if !slices.Contains([]wire.Outcome{"accepted", "committed", "rejected", "conflicted"}, op.Outcome) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.state.Operations[id]
	if s.client != c || s.generation != generation || !exists || current.Path != j.Path || current.Digest != j.Digest {
		return errors.New("original operation changed during reconciliation")
	}
	if current.Outcome != "unknown" {
		if current.Outcome == string(op.Outcome) {
			return nil // Another original-owner read settled the same receipt.
		}
		return errors.New("original operation outcome changed during reconciliation")
	}
	j = current
	j.Outcome = string(op.Outcome)
	j.PendingInput = nil
	if op.Target != nil {
		j.TurnID = value(op.Target.TurnId)
	}
	if !strings.HasSuffix(j.Path, "/steer") {
		j.Body = nil
	}
	if op.Resource != nil {
		j.Resource = value(op.Resource.Ref)
	}
	if j.Path == "/sessions" && value(op.SessionId) != "" {
		j.Resource = value(op.SessionId)
	}
	previousContext, previousReceipt := clone(s.state.Context), s.state.LastReceipt
	s.state.Operations[id] = j
	s.state.Context.Resolve(id, productOutcome(op.Outcome))
	mainInput := "/sessions/" + idPath(s.state.Session.SessionId)
	if !j.Scheduled && (j.Path == mainInput+"/prompt" || j.Path == mainInput+"/steer") &&
		(s.state.LastReceipt.ID == "" || s.state.LastReceipt.ID == id) {
		s.state.LastReceipt = api.Receipt{ID: id, Outcome: productOutcome(op.Outcome)}
	}
	if err := s.saveLocked(); err != nil {
		s.state.Operations[id] = current
		s.state.Context, s.state.LastReceipt = previousContext, previousReceipt
		return err
	}
	s.cleanupContextLocked()
	s.bumpLocked()
	return nil
}

// Session progress is push-driven. Maintenance only restores a failed stream,
// observes Host identity, or reconciles an unresolved operation/one terminal hook.
func (s *Session) needsRefreshLocked() bool {
	return len(s.pendingObservationsLocked()) != 0
}
