package caelis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (s *Session) Snapshot() api.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}
func (s *Session) Revision() uint64 { s.mu.Lock(); defer s.mu.Unlock(); return s.revision }
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
	if s.connected && !s.closed {
		out.Connection = "ready"
		out.ConnectionIssue = ""
		out.Phase = "idle"
	}
	v := s.state.Views[s.state.Bot.SessionId]
	if v != nil {
		out.Items = clone(v.Items)
		out.CurrentTurn = value(v.State.Run.TurnId)
		if out.CurrentTurn == "" && len(v.Items) > 0 {
			out.CurrentTurn = v.Items[len(v.Items)-1].TurnKey
		}
		if s.connected && !value(v.State.Run.Active) {
			switch value(v.State.Run.Status) {
			case "completed", "failed", "interrupted":
				out.Phase = value(v.State.Run.Status)
			case "cancelled":
				out.Phase = "interrupted"
			}
		}
		if s.connected && value(v.State.Run.Active) {
			out.Phase = "working"
			out.CanInterrupt = true
		}
	}
	if s.connected {
		for sid, v := range s.state.Views {
			a := v.State.Approval.Active
			if a == nil {
				continue
			}
			raw, _ := json.Marshal(a.Permission)
			var p nativePermission
			_ = json.Unmarshal(raw, &p)
			summary := p.ToolCall.Title
			details, _ := json.MarshalIndent(p.ToolCall.RawInput, "", "  ")
			item := api.Approval{ID: approvalID(s.state.InstanceID, sid, a), Title: summary, Action: summary, Description: "Caelis 请求执行授权", Details: string(details), Status: "pending", Choices: []api.Choice{}}
			if sid != s.state.Bot.SessionId {
				item.Description = "独立工作任务请求授权"
			}
			for _, o := range p.Options {
				item.Choices = append(item.Choices, api.Choice{ID: o.ID, Label: o.Name, Scope: o.Kind})
			}
			out.Approvals = append(out.Approvals, item)
		}
	}
	slices.SortFunc(out.Approvals, func(a, b api.Approval) int { return strings.Compare(a.ID, b.ID) })
	if len(out.Approvals) > 0 {
		out.Phase = "waiting_approval"
	}
	unknown := false
	for _, j := range s.state.Operations {
		if j.Outcome == "unknown" {
			unknown = true
			break
		}
	}
	if unknown && s.connected {
		out.Phase = "unknown"
		out.Message = "有一次操作的结果尚未确认，正在核对；不会自动重发。"
	}
	out.CanSend = s.connected && !s.closed && !unknown && v != nil && !value(v.State.Run.Active) && v.State.Approval.Active == nil
	// Worker approval does not turn the secretary's input into a steer action.
	out.CanSteer = false
	return out
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
		if a := v.State.Approval.Active; a != nil && approvalID(s.state.InstanceID, sid, a) == id {
			return approvalRef{sid, clone(a)}, true
		}
	}
	return approvalRef{}, false
}
func applyEnvelope(v *view, e wire.Envelope) {
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
	isMain := (value(e.Scope) == "" || value(e.Scope) == "main") && value(e.ApprovalRequestId) == ""
	if e.Lifecycle != nil && e.Kind == "caelis/lifecycle" && isMain {
		switch e.Lifecycle.State {
		case "running", "started":
			v.State.Run.Active = pointer(true)
		case "completed", "failed", "cancelled", "interrupted", "stopped":
			v.State.Run.Active = pointer(false)
			v.State.Approval = wire.ApprovalState{}
		}
		v.State.Run.Status = &e.Lifecycle.State
	}
	if e.TurnId != nil && isMain {
		v.State.Run.TurnId = e.TurnId
	}
	if e.HandleId != nil && isMain {
		v.State.Run.HandleId = e.HandleId
	}
	if e.RunId != nil && isMain {
		v.State.Run.RunId = e.RunId
	}
	if !isMain || len(value(e.Update)) == 0 {
		return
	}
	var update struct {
		Kind       string                      `json:"sessionUpdate"`
		Content    struct{ Type, Text string } `json:"content"`
		MessageID  string                      `json:"messageId"`
		Title      string                      `json:"title"`
		ToolCallID string                      `json:"toolCallId"`
		Status     string                      `json:"status"`
	}
	if json.Unmarshal(value(e.Update), &update) != nil {
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
		// The pinned Host emits Bot configuration as an out-of-turn canonical
		// user event. It instructs the model, but is not a chat submission.
		// Use native provenance, never match generated or user-written prose.
		if value(e.TurnId) == "" && value(e.ActivityId) == "" && strings.HasPrefix(value(e.EventId), "bot-config-") {
			return
		}
		kind = "user"
	case "agent_message_chunk":
		kind = "assistant"
	default:
		return
	}
	if update.Content.Type != "text" || update.Content.Text == "" {
		return
	}
	turn := value(e.TurnId)
	if turn == "" {
		turn = value(e.ActivityId)
	}
	id := turn + "/" + kind
	if update.MessageID != "" {
		id += "/" + update.MessageID
	}
	for i := len(v.Items) - 1; i >= 0; i-- {
		if v.Items[i].ID == id {
			v.Items[i].Text += update.Content.Text
			return
		}
	}
	v.Items = append(v.Items, api.Item{ID: id, TurnKey: turn, Kind: kind, Text: update.Content.Text, Status: "completed"})
}
func (s *Session) ensureStreamLocked(sid string) {
	if s.streams[sid] || s.ctx == nil || s.closed {
		return
	}
	s.streams[sid] = true
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
		if err := s.watch(ctx, c, sid, instance); err != nil && ctx.Err() == nil {
			s.fail(err)
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
	path := "/sessions/" + idPath(sid) + "/reconnect?history_turns=64"
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
			v.State = st
			v.Observed++
			s.bumpLocked()
			return nil
		}
		if f.event == "caelis.control.done" {
			return errors.New("观察结束")
		}
		if f.event != "caelis.control.delivery" {
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
			snapshotID = value(d.SnapshotId)
			page = 0
			staged = &view{State: *bootstrap, Items: []api.Item{}, Seen: map[string]bool{}}
			return nil
		case "replace_page":
			if staged == nil || snapshotID != value(d.SnapshotId) || value(d.Page) != page || d.Source != "replacement" || value(d.NextCursor) != "" {
				return errors.New("恢复页面不连续")
			}
			page++
			for _, e := range d.Events {
				applyEnvelope(staged, e)
			}
			return nil
		case "replace_end":
			if staged == nil || snapshotID != value(d.SnapshotId) || value(d.Page) != page || d.Source != "replacement" || len(d.Events) != 0 || value(d.NextCursor) != "" {
				return errors.New("恢复快照不匹配")
			}
			staged.State = *bootstrap
			v = staged
			s.state.Views[sid] = v
			staged = nil
		case "append_page":
			if staged != nil {
				return errors.New("替换未完成")
			}
			for _, e := range d.Events {
				applyEnvelope(v, e)
			}
		case "sync", "status":
			if staged != nil {
				return errors.New("替换未完成")
			}
		default:
			return fmt.Errorf("未知状态投影 %s", d.Kind)
		}
		v.Observed++
		if d.NextCursor != nil {
			v.Cursor = *d.NextCursor
		}
		if e := s.saveLocked(); e != nil {
			return e
		}
		s.bumpLocked()
		return nil
	})
}
func (s *Session) pollLoop(ctx context.Context) {
	defer s.wg.Done()
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.step.Lock()
		e := s.refresh(ctx)
		s.step.Unlock()
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
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	c := s.client
	life := s.state.Client
	connected := s.connected
	s.mu.Unlock()
	if c == nil || !connected {
		if e := s.connect(ctx); e != nil {
			return e
		}
		s.mu.Lock()
		s.connected = true
		s.issue = ""
		c = s.client
		life = s.state.Client
		s.mu.Unlock()
	}
	info, e := initialize(ctx, c)
	if e != nil {
		return e
	}
	if value(info.InstanceId) != s.state.InstanceID || value(info.StoreId) != s.state.StoreID {
		return errors.New("Caelis Host 已变化，正在重新核对绑定")
	}
	if !life.ExpiresAt.After(time.Now().Add(3 * time.Minute)) {
		var renewed wire.BotClient
		if e = c.json(ctx, "POST", s.botPath("/client/renew"), struct{}{}, &renewed, "", ""); e != nil {
			return e
		}
		s.mu.Lock()
		s.state.Client = renewed
		s.mu.Unlock()
	}
	var works []wire.BotWork
	if e = c.json(ctx, "GET", s.botPath("/work"), nil, &works, "", ""); e != nil {
		return e
	}
	var completions []wire.BotCompletion
	if e = c.json(ctx, "GET", s.botPath("/completions"), nil, &completions, "", ""); e != nil {
		return e
	}
	s.mu.Lock()
	s.works = works
	s.completions = completions
	s.ensureDesktopStreamLocked()
	sessions := []string{s.state.Bot.SessionId}
	for _, w := range works {
		sessions = append(sessions, w.SessionId)
	}
	s.mu.Unlock()
	for _, sid := range sessions {
		s.mu.Lock()
		before := s.state.Views[sid]
		var observed uint64
		if before != nil {
			observed = before.Observed
		}
		s.mu.Unlock()
		var state wire.SessionState
		if e = c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/state", nil, &state, "", ""); e != nil {
			return e
		}
		if state.SessionId != sid {
			return errors.New("工作状态来源不匹配")
		}
		s.mu.Lock()
		v := s.state.Views[sid]
		if v == nil {
			v = &view{Items: []api.Item{}, Seen: map[string]bool{}}
			s.state.Views[sid] = v
		}
		// An HTTP response can arrive after a newer SSE lifecycle fact. Do
		// not move execution back in time; the next poll reconciles approvals.
		if v == before && v.Observed == observed || before == nil && v.Observed == 0 {
			v.State = state
		}
		s.ensureStreamLocked(sid)
		s.mu.Unlock()
	}
	if e = s.recoverPrompts(ctx); e != nil {
		return e
	}
	if e = s.desktopTick(ctx); e != nil {
		return e
	}
	s.mu.Lock()
	e = s.saveLocked()
	s.bumpLocked()
	s.mu.Unlock()
	return e
}
func (s *Session) recoverPrompts(ctx context.Context) error {
	s.mu.Lock()
	ops := clone(s.state.Operations)
	s.mu.Unlock()
	for id, j := range ops {
		if j.Outcome != "unknown" || !strings.HasSuffix(j.Path, "/prompt") {
			continue
		}
		var src wire.BotRequestSource
		e := s.client.json(ctx, "GET", s.botPath("/requests/")+idPath(id), nil, &src, "", "")
		if e != nil {
			continue
		}
		if src.OperationId != id || src.BotId != s.state.Bot.Id || src.Id == "" || src.Execution.InstanceId == "" || value(src.ClientId) != s.state.Client.Id || src.Execution.RunId == "" || src.Execution.HandleId == "" || src.Execution.TurnId == "" || src.Execution.SessionId != s.state.Bot.SessionId || src.PrincipalId != s.state.PrincipalID {
			continue
		}
		s.mu.Lock()
		j.Outcome = "accepted"
		j.Body = nil
		s.state.Operations[id] = j
		s.state.LastReceipt = api.Receipt{ID: id, Outcome: "accepted"}
		e = s.saveLocked()
		s.mu.Unlock()
		if e != nil {
			return e
		}
	}
	return nil
}
