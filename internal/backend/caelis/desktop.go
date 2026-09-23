package caelis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (s *Session) desktopTick(ctx context.Context) error {
	var snap wire.BotDesktopSnapshot
	if e := s.client.json(ctx, "GET", s.botPath("/client/actions"), nil, &snap, "", ""); e != nil {
		return e
	}
	for _, call := range snap.Calls {
		if e := s.dispatch(ctx, call); e != nil {
			return e
		}
	}
	s.mu.Lock()
	s.state.DesktopCursor = snap.Cursor
	s.mu.Unlock()
	var grants []wire.BotReminderGrant
	if e := s.client.json(ctx, "GET", s.botPath("/client/reminders"), nil, &grants, "", ""); e != nil {
		return e
	}
	var occurrences []wire.BotReminderFire
	if e := s.client.json(ctx, "GET", s.botPath("/client/reminder-occurrences"), nil, &occurrences, "", ""); e != nil {
		return e
	}
	if e := s.reconcileReminderReceipts(occurrences); e != nil {
		return e
	}
	for _, g := range grants {
		due := grantDue(g, time.Now())
		if !g.Active || due.IsZero() || due.After(time.Now()) {
			continue
		}
		seen := false
		for _, o := range occurrences {
			if o.GrantId == g.Id && o.Version == g.Version && o.Due.Equal(due) {
				seen = true
				break
			}
		}
		if seen {
			continue
		}
		hash := sha256.Sum256([]byte(g.Id + "\x00" + g.Version + "\x00" + due.UTC().Format(time.RFC3339)))
		op := "reminder-" + hex.EncodeToString(hash[:])
		req := wire.BotReminderRequest{BotId: s.state.Bot.Id, SessionId: pointer(s.state.Bot.SessionId), GrantId: g.Id, Version: g.Version, Due: due, OperationId: &op}
		out, e := s.command(ctx, op, s.botPath("/client/reminders/fire"), req)
		if e != nil {
			return e
		}
		if succeeded(out.Outcome) {
			s.notifyOnce(op, value(g.Arguments.Label), true)
		}
	}
	for _, n := range s.completions {
		if n.ReportState == "admitted" {
			s.notifyOnce(n.Id, "工作已完成", false)
		}
	}
	return nil
}

// A persisted occurrence proves queue admission, not model execution. In
// particular, a claimed occurrence is never dispatched again after a crash.
func (s *Session) reconcileReminderReceipts(occurrences []wire.BotReminderFire) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for id, j := range s.state.Operations {
		if j.Outcome != "unknown" || j.Path != s.botPath("/client/reminders/fire") {
			continue
		}
		var req wire.BotReminderRequest
		if json.Unmarshal(j.Body, &req) != nil || value(req.OperationId) != id {
			continue
		}
		for _, o := range occurrences {
			if o.BotId != s.state.Bot.Id || o.PrincipalId != s.state.PrincipalID || o.ClientId != s.state.Client.Id || o.GrantId != req.GrantId || o.Version != req.Version || !o.Due.Equal(req.Due) {
				continue
			}
			j.Outcome, j.Resource, j.Body = "committed", o.Id, nil
			s.state.Operations[id] = j
			changed = true
			break
		}
	}
	if changed {
		s.bumpLocked()
		return s.saveLocked()
	}
	return nil
}

func (s *Session) notifyOnce(id, title string, reminder bool) {
	s.mu.Lock()
	if s.state.Notified[id] {
		s.mu.Unlock()
		return
	}
	s.state.Notified[id] = true
	e := s.saveLocked()
	notify := s.effects.Notify
	s.mu.Unlock()
	if e == nil && notify != nil {
		notify(id, title, "打开对话查看", reminder)
	}
}
func (s *Session) dispatch(ctx context.Context, call wire.BotDesktopCall) error {
	if call.ActivationId != s.state.Client.ActivationId || call.ClientId != s.state.Client.Id || call.BotId != s.state.Bot.Id || call.PrincipalId != s.state.PrincipalID {
		return nil
	}
	s.mu.Lock()
	r, known := s.state.Actions[call.Id]
	s.mu.Unlock()
	if call.State == "completed" || call.State == "cancelled" || call.State == "failed" {
		return nil
	}
	if known {
		// Only a durably stored receipt may be re-delivered. Never repeat a claim or effect.
		if r.Phase != "receipt" || r.Receipt == nil {
			return nil
		}
		return s.deliverAction(ctx, call.Id, r)
	}
	if call.State != "queued" {
		return nil
	}
	r = actionRecord{Call: call, Phase: "claim-unknown"}
	s.mu.Lock()
	s.state.Actions[call.Id] = r
	e := s.saveLocked()
	s.mu.Unlock()
	if e != nil {
		return e
	}
	var claim wire.BotDesktopClaim
	if e = s.client.json(ctx, "POST", s.botPath("/client/actions/")+idPath(call.Id)+"/claim", struct{}{}, &claim, "", ""); e != nil {
		return e
	}
	if claim.Call.Id != call.Id || claim.Call.ActivationId != call.ActivationId || claim.Token == "" || claim.Call.ClientId != call.ClientId || claim.Call.BotId != call.BotId || claim.Call.PrincipalId != call.PrincipalId || claim.Call.SourceId != call.SourceId || claim.Call.Execution != call.Execution || claim.Call.Action != call.Action || !reflect.DeepEqual(claim.Call.Arguments, call.Arguments) {
		return errors.New("Caelis 动作领取回执不匹配")
	}
	r.Phase = "executing"
	r.Receipt = &wire.BotDesktopReceipt{Token: claim.Token}
	s.mu.Lock()
	s.state.Actions[call.Id] = r
	e = s.saveLocked()
	effects := s.effects
	s.mu.Unlock()
	if e != nil {
		return e
	}
	args, _ := json.Marshal(claim.Call.Arguments)
	var result json.RawMessage
	if effects.Execute == nil {
		e = errors.New("桌面能力不可用")
	} else {
		result, e = effects.Execute(claim.Call.Action, args)
	}
	if e != nil {
		r.Receipt.IsError = pointer(true)
		r.Receipt.Result = map[string]string{"error": "本地动作未完成"}
	} else if len(result) > 65536 || !json.Valid(result) {
		r.Receipt.IsError = pointer(true)
		r.Receipt.Result = map[string]string{"error": "本地动作结果无效"}
	} else {
		r.Receipt.Result = result
	}
	r.Phase = "receipt"
	s.mu.Lock()
	s.state.Actions[call.Id] = r
	e = s.saveLocked()
	s.mu.Unlock()
	if e != nil {
		return e
	}
	return s.deliverAction(ctx, call.Id, r)
}
func (s *Session) deliverAction(ctx context.Context, id string, r actionRecord) error {
	var out wire.BotDesktopCall
	e := s.client.json(ctx, "POST", s.botPath("/client/actions/")+idPath(id)+"/result", r.Receipt, &out, "", "")
	if e != nil {
		return e
	}
	if out.Id != id {
		return errors.New("动作确认不匹配")
	}
	r.Phase = "done"
	r.Receipt = nil
	s.mu.Lock()
	s.state.Actions[id] = r
	e = s.saveLocked()
	s.mu.Unlock()
	return e
}

// grantDue chooses at most one authorized catch-up occurrence, using Control's
// grant epoch rather than the local save time. Control still validates admission.
func grantDue(g wire.BotReminderGrant, now time.Time) time.Time {
	a := g.Arguments
	after := g.CreatedAt
	if g.LastOccurrence.After(after) {
		after = g.LastOccurrence
	}
	if at := value(a.At); at != "" {
		t, e := time.Parse(time.RFC3339, at)
		if e == nil && t.After(g.LastOccurrence) {
			return t
		}
		return time.Time{}
	}
	if n := value(a.EveryMinutes); n > 0 {
		interval := time.Duration(n) * time.Minute
		first := g.CreatedAt.Add(interval)
		if first.After(now) {
			return first
		}
		due := first.Add(now.Sub(first) / interval * interval)
		if due.After(g.LastOccurrence) {
			return due
		}
		return time.Time{}
	}
	if daily := value(a.Daily); daily != "" {
		var h, m int
		if _, e := fmt.Sscanf(daily, "%d:%d", &h, &m); e != nil {
			return time.Time{}
		}
		loc, e := time.LoadLocation(value(a.TimeZone))
		if e != nil {
			return time.Time{}
		}
		local := now.In(loc)
		for back := 0; back < 4; back++ {
			d := local.AddDate(0, 0, -back)
			t := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc)
			if t.Hour() == h && t.Minute() == m && !t.After(now) && t.After(after) {
				return t
			}
		}
	}
	return time.Time{}
}

// The action stream only wakes the serialized native dispatcher. Observation is
// never authority to execute, and its resume cursor is separate from chat.
func (s *Session) ensureDesktopStreamLocked() {
	const key = "@desktop"
	if s.streams[key] || s.ctx == nil || s.closed {
		return
	}
	if s.streamCtx == nil {
		s.streamCtx, s.streamCancel = context.WithCancel(s.ctx)
	}
	s.streams[key] = true
	ctx, c, generation := s.streamCtx, s.client, s.generation
	path, cursor := s.botPath("/client/actions/events"), s.state.DesktopCursor
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			if s.generation == generation {
				s.streams[key] = false
			}
			s.mu.Unlock()
		}()
		_ = c.stream(ctx, path, cursor, func(f frame) error {
			if f.event != "bot.desktop.snapshot" {
				return errors.New("动作观察需要恢复")
			}
			var snap wire.BotDesktopSnapshot
			if json.Unmarshal(f.data, &snap) != nil || f.id != snap.Cursor {
				return errors.New("动作游标不匹配")
			}
			select {
			case s.wake <- struct{}{}:
			default:
			}
			return nil
		})
	}()
}
