package codex

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const historyPageSize = 20

type turnPage struct {
	Data       []nativeTurn `json:"data"`
	NextCursor string       `json:"nextCursor"`
}

func unsupportedHistory(err error) bool {
	var native *NativeError
	return errors.As(err, &native) && (native.Code == -32601 || native.Code == -32602)
}

// Match the specific native lifecycle error, not every invalid-request error.
func nativeThreadError(err error, prefix, thread string) bool {
	var native *NativeError
	return thread != "" && errors.As(err, &native) && native.Code == -32600 && native.Message == prefix+thread
}
func readTurnPage(ctx context.Context, c *Client, thread, cursor string) (turnPage, error) {
	var page turnPage
	err := callDecode(ctx, c, "thread/turns/list", map[string]any{
		"threadId": thread, "cursor": cursorOrNil(cursor), "limit": historyPageSize, "sortDirection": "desc", "itemsView": "full",
	}, &page)
	if err == nil && (page.Data == nil || len(page.Data) > historyPageSize || (page.NextCursor != "" && page.NextCursor == cursor)) {
		err = ErrProtocol
	}
	return page, err
}
func cursorOrNil(cursor string) any {
	if cursor == "" {
		return nil
	}
	return cursor
}
func chronological(turns []nativeTurn) []nativeTurn {
	turns = slices.Clone(turns)
	slices.Reverse(turns)
	return turns
}

// Reading older messages must not replay lifecycle, approvals, or child creation.
// A separate projector extracts only user/assistant content and result handles.
func (s *Session) LoadEarlier(ctx context.Context) error {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	ctx, cancel := s.operation(ctx, 15*time.Second)
	defer cancel()
	s.mu.Lock()
	c, thread, cursor, epoch := s.client, s.historyThread, s.historyCursor, s.epoch
	if thread == "" {
		thread = s.binding.ThreadID
	}
	previous := s.historyPrevious
	if cursor == "" && previous >= 0 && previous < len(s.binding.PastThreads) {
		thread = s.binding.PastThreads[previous]
		previous--
	} else if cursor == "" {
		s.mu.Unlock()
		return nil
	}
	ready := s.state.Connection == "ready"
	scheduled := maps.Clone(s.binding.Scheduled)
	inputs := maps.Clone(s.binding.ContextInputs)
	s.mu.Unlock()
	if !ready || c == nil {
		return errors.New("请先恢复连接，再查看更早消息")
	}
	page, err := readTurnPage(ctx, c, thread, cursor)
	if unsupportedHistory(err) && cursor == "" {
		var response struct {
			Thread nativeThread `json:"thread"`
		}
		err = callDecode(ctx, c, "thread/read", map[string]any{"threadId": thread, "includeTurns": true}, &response)
		if err == nil && response.Thread.ID != thread {
			err = ErrProtocol
		}
		page.Data = slices.Clone(response.Thread.Turns)
		slices.Reverse(page.Data)
	}
	if err != nil {
		return errors.New("更早消息暂时无法读取，请重试")
	}
	projection := &Session{opts: s.opts, binding: binding{Scheduled: scheduled, ContextInputs: inputs}}
	projection.resetProjection()
	for _, turn := range chronological(page.Data) {
		projection.runs[turn.ID] = turn.Status
		for _, item := range turn.Items {
			if item.Type == "userMessage" || item.Type == "agentMessage" {
				projection.applyItem(turn.ID, item, terminal(turn.Status))
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch != epoch || s.client != c || s.historyCursor != cursor {
		return errors.New("连接已更新，请重新读取")
	}
	for id, turn := range projection.binding.Scheduled {
		if turn != "" {
			s.binding.Scheduled[id] = turn
		}
	}
	for turn, status := range projection.runs {
		s.runs[turn] = status
	}
	older := make([]api.Item, 0, len(projection.state.Items))
	for _, item := range projection.state.Items {
		if _, exists := s.items[item.ID]; !exists {
			older = append(older, item)
		}
	}
	s.state.Items = append(older, s.state.Items...)
	for i, item := range s.state.Items {
		s.items[item.ID] = i
	}
	for id, path := range projection.artifacts {
		if _, exists := s.artifacts[id]; !exists {
			s.artifacts[id] = path
		}
	}
	s.historyThread, s.historyPrevious = thread, previous
	s.historyCursor, s.state.HasEarlier = page.NextCursor, page.NextCursor != "" || previous >= 0
	s.update()
	return nil
}

func (s *Session) Revision() uint64 { s.mu.Lock(); defer s.mu.Unlock(); return s.state.Revision }

// Native observers and pet polling do not need hydrated older messages.
func (s *Session) RecentSnapshot() api.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.state
	start := 0
	for i := len(v.Items) - 1; i >= 0; i-- {
		if v.Items[i].Kind == "user" || v.Items[i].Kind == "activation" {
			start = i
			break
		}
	}
	// Steering adds another user item in the same turn. Retain earlier tool
	// owners until the service selects the current activity, then trim prose.
	if v.CurrentTurn != "" {
		for i := 0; i < start; i++ {
			if v.Items[i].TurnKey == v.CurrentTurn {
				start = i
				break
			}
		}
	}
	v.Items = v.Items[start:]
	b, _ := json.Marshal(v)
	var out api.Snapshot
	_ = json.Unmarshal(b, &out)
	return s.presentScheduled(out)
}

// ComposerSnapshot never traverses or serializes the transcript. Only interaction
// authority and value-only reference metadata cross the quick-input boundary.
func (s *Session) ComposerSnapshot() api.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.state
	v.Items = []api.Item{}
	v.Reviews = []api.Review{}
	v.References = append([]api.Reference{}, s.state.References...)
	v.Approvals = []api.Approval{}
	for _, a := range s.state.Approvals {
		if a.Status != "resolved" {
			v.Approvals = append(v.Approvals, api.Approval{Status: a.Status})
		}
	}
	return v
}
