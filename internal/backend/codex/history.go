package codex

import (
	"context"
	"encoding/json"
	"errors"

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
func cursorOrNil(cursor string) any {
	if cursor == "" {
		return nil
	}
	return cursor
}

// Reading older messages must not replay lifecycle, approvals, or child creation.
// A separate projector extracts only user/assistant content and result handles.
func (s *Session) LoadEarlier(context.Context) error { return nil }

// Only execution metadata and the latest human-message summary are observed.
// Bot never reloads tool history or uses a worker's payload size as admission.
func readThreadState(ctx context.Context, c *Client, id string) (nativeThread, error) {
	var response struct {
		Thread nativeThread `json:"thread"`
	}
	if err := callDecode(ctx, c, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &response); err != nil {
		return nativeThread{}, err
	}
	if response.Thread.ID != id {
		return nativeThread{}, ErrProtocol
	}
	// Compatibility peers may still include turns despite the explicit request.
	if len(response.Thread.Turns) > 1 {
		response.Thread.Turns = response.Thread.Turns[len(response.Thread.Turns)-1:]
	}
	var recent turnPage
	err := callDecode(ctx, c, "thread/turns/list", map[string]any{"threadId": id, "limit": 1, "sortDirection": "desc", "itemsView": "summary"}, &recent)
	if unsupportedHistory(err) {
		return response.Thread, nil
	}
	if err != nil {
		return response.Thread, err
	}
	if recent.Data == nil || len(recent.Data) > 1 {
		return response.Thread, ErrProtocol
	}
	if len(recent.Data) > 0 || len(response.Thread.Turns) == 0 {
		response.Thread.Turns = recent.Data
	}
	return response.Thread, nil
}

func (s *Session) Revision() uint64 {
	if v := s.cached.Load(); v != nil {
		return v.Revision
	}
	return 0
}

// Native observers and pet polling do not need hydrated older messages.
func (s *Session) RecentSnapshot() api.Snapshot {
	v := s.Snapshot()
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
	return out
}

// ComposerSnapshot never traverses or serializes the transcript. Only interaction
// authority and value-only reference metadata cross the quick-input boundary.
func (s *Session) ComposerSnapshot() api.Snapshot {
	if s.mu.TryLock() {
		v := s.composerLocked()
		s.cachedComposer.Store(&v)
		s.mu.Unlock()
	}
	if v := s.cachedComposer.Load(); v != nil {
		b, _ := json.Marshal(v)
		var out api.Snapshot
		_ = json.Unmarshal(b, &out)
		return out
	}
	return api.Snapshot{Connection: "offline", Items: []api.Item{}, Approvals: []api.Approval{}}
}
func (s *Session) composerLocked() api.Snapshot {
	v := s.state
	v.Items = []api.Item{}
	v.Reviews = []api.Review{}
	v.References = append([]api.Reference{}, v.References...)
	approvals := v.Approvals
	v.Approvals = []api.Approval{}
	for _, a := range approvals {
		if a.Status != "resolved" {
			v.Approvals = append(v.Approvals, api.Approval{Status: a.Status})
		}
	}
	return v
}
