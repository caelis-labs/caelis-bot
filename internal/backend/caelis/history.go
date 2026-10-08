package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

const (
	historyPageTurns = 8
	// Each projected SSE frame is already limited to 8 MiB. Bound the whole
	// user-requested page too, before it can grow the private transcript.
	historyPageBytes = 16 << 20
)

// History tokens belong to a backward display traversal, never to the live
// command cursor. Context renewal retains that traversal in its original view.
func (s *Session) earlierViewLocked() (string, *view) {
	for _, sid := range s.state.PastSessions {
		if v := s.state.Views[sid]; v != nil && v.HistoryBefore != "" {
			return sid, v
		}
	}
	sid := s.state.Session.SessionId
	v := s.state.Views[sid]
	if v != nil && v.HistoryBefore != "" {
		return sid, v
	}
	return "", nil
}

var historyComplete = errors.New("history page complete")

// Older history is projected off-screen and committed only at the finite
// stream's sync boundary. It cannot update live approvals, usage, commands,
// operation journals or lifecycle, and cannot execute application callbacks.
func (s *Session) LoadEarlier(ctx context.Context) error {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	s.mu.Lock()
	sid, original := s.earlierViewLocked()
	if original == nil {
		s.mu.Unlock()
		return nil
	}
	if !s.connected || s.closed || s.client == nil {
		s.mu.Unlock()
		return errors.New("请先恢复连接，再查看更早消息")
	}
	c, instance, generation, before := s.client, s.state.InstanceID, s.generation, original.HistoryBefore
	scheduled := map[string]bool{}
	hostNotices := map[string]bool{}
	hostTurns := map[string]bool{}
	for id, op := range s.state.Operations {
		if op.Scheduled {
			scheduled[id] = true
		}
		if op.Source.Kind == "application_summary" {
			hostNotices[id] = true
			if op.TurnID != "" {
				hostTurns[op.TurnID] = true
			}
		}
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	projection := &view{State: wire.SessionState{SessionId: sid}, Seen: map[string]bool{}, Items: []api.Item{}}
	next, snapshot, page, bootstrap, replaced, appended := "", "", 0, false, false, false
	pageBytes := 0
	path := "/sessions/" + idPath(sid) + "/reconnect?history_turns=" + strconv.Itoa(historyPageTurns) + "&history_before=" + url.QueryEscape(before)
	err := c.stream(ctx, path, "", func(f frame) error {
		pageBytes += len(f.data)
		if pageBytes > historyPageBytes {
			return errors.New("历史页面超过大小限制")
		}
		if !bootstrap {
			var state wire.SessionState
			if f.event != "caelis.control.bootstrap" || json.Unmarshal(f.data, &state) != nil || state.SessionId != sid || state.ProtocolVersion != 1 || state.ApiVersion != "v1" || state.EnvelopeVersion != "caelis.control.envelope/v1" {
				return errors.New("历史恢复状态不匹配")
			}
			bootstrap = true
			return nil
		}
		var d wire.SessionFeedDelivery
		if f.event != "caelis.control.delivery" || json.Unmarshal(f.data, &d) != nil || f.id != "" && f.id != value(d.NextCursor) || d.Kind != "sync" && d.HistoryBefore != nil {
			return errors.New("历史页面不匹配")
		}
		if d.Kind != "append_page" && value(d.NextCursor) != "" {
			return errors.New("历史页面包含实时游标")
		}
		switch d.Kind {
		case "replace_begin":
			if snapshot != "" || replaced || appended || d.Source != "replacement" || value(d.SnapshotId) == "" || value(d.Page) != 0 || len(d.Events) != 0 {
				return errors.New("历史替换无效")
			}
			snapshot = value(d.SnapshotId)
		case "replace_page":
			if snapshot == "" || replaced || d.Source != "replacement" || value(d.SnapshotId) != snapshot || value(d.Page) != page {
				return errors.New("历史页面不连续")
			}
			page++
			for _, e := range d.Events {
				applyEnvelope(projection, e, scheduled[value(e.InputOperationId)], hostNotices[value(e.InputOperationId)] || value(e.InputOperationId) == "" && hostTurns[value(e.TurnId)])
			}
		case "replace_end":
			if snapshot == "" || replaced || d.Source != "replacement" || value(d.SnapshotId) != snapshot || value(d.Page) != page || len(d.Events) != 0 {
				return errors.New("历史替换未完成")
			}
			replaced = true
		case "append_page":
			if snapshot != "" || d.Source != "exact" || value(d.SnapshotId) != "" || value(d.Page) != 0 {
				return errors.New("历史追加无效")
			}
			appended = true
			for _, e := range d.Events {
				applyEnvelope(projection, e, scheduled[value(e.InputOperationId)], hostNotices[value(e.InputOperationId)] || value(e.InputOperationId) == "" && hostTurns[value(e.TurnId)])
			}
		case "sync":
			if snapshot != "" && !replaced || d.Source != "exact" || value(d.SnapshotId) != "" || len(d.Events) != 0 || value(d.HistoryBefore) == before {
				return errors.New("历史边界无效")
			}
			next = value(d.HistoryBefore)
			return historyComplete
		default:
			return errors.New("历史页面类型不匹配")
		}
		return nil
	})
	if !errors.Is(err, historyComplete) {
		return errors.New("更早消息暂时无法读取，请重试")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.client != c || s.state.InstanceID != instance || s.generation != generation || s.state.Views[sid] != original || original.HistoryBefore != before {
		return errors.New("连接已更新，请重新读取")
	}
	seen := make(map[string]bool, len(original.Items))
	for _, item := range original.Items {
		seen[item.ID] = true
	}
	older := make([]api.Item, 0, len(projection.Items))
	for _, item := range projection.Items {
		if !seen[item.ID] {
			older = append(older, item)
			seen[item.ID] = true
		}
	}
	items, reviews, liveReviews := original.Items, original.Reviews, original.LiveReviews
	original.Items = append(older, original.Items...)
	original.HistoryBefore = next
	if len(projection.Reviews) > 0 {
		merged := make(map[string]reviewFact, len(original.Reviews)+len(projection.Reviews))
		for id, fact := range original.Reviews {
			merged[id] = fact
		}
		var live map[string]reviewFact
		for id, fact := range original.LiveReviews {
			if live == nil {
				live = make(map[string]reviewFact, len(original.LiveReviews))
			}
			live[id] = fact
		}
		for id, fact := range projection.Reviews {
			if fact.Status != "approved" && fact.Status != "denied" || id != reviewID(sid, fact.TurnID, fact.ApprovalID) {
				continue
			}
			if _, exists := merged[id]; !exists {
				merged[id] = fact
			}
			delete(live, id)
		}
		original.Reviews, original.LiveReviews = merged, live
	}
	rollback := func() {
		original.Items, original.HistoryBefore = items, before
		original.Reviews, original.LiveReviews = reviews, liveReviews
	}
	// loadBinding refuses files above 64 MiB. Keep an explicit older-page load
	// from persisting a document that would make the next normal start fail.
	encoded, err := json.Marshal(s.state)
	if err != nil || len(encoded) >= 64<<20 {
		rollback()
		return errors.New("历史页面超过本地保存限制")
	}
	// Display-only history must not run saveLocked's background-completion hooks.
	if err := privateWrite(s.path, s.state); err != nil {
		rollback()
		return err
	}
	s.bumpLocked()
	return nil
}
