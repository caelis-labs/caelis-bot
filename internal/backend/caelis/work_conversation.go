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

// ReadWorkConversation traverses the original native Worker Session without
// importing history into resident chat or acknowledging a completion notice.
func (s *Session) ReadWorkConversation(ctx context.Context, id string) ([]api.TaskConversationMessage, error) {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	s.mu.Lock()
	w, owned := s.state.Workers[id]
	c, connected := s.client, s.connected && !s.closed
	sid, generation, instance := w.Binding.SessionId, s.generation, s.state.InstanceID
	s.mu.Unlock()
	if !owned {
		return nil, errors.New("only Bot-created tasks can be read")
	}
	if sid == "" || c == nil || !connected {
		return nil, errors.New("original native Worker transcript is not readable; retain the task handle and retry after reconnect")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var ordered []api.TaskConversationMessage
	seen := map[string]bool{}
	before := ""
	readTurns := map[string]bool{}
	cursors := map[string]bool{}
	for pageNumber := 0; pageNumber < 1000; pageNumber++ {
		projection := newWorkerTranscript(sid)
		pageTurns := map[string]bool{}
		eventKeys := map[string]bool{}
		apply := func(event wire.Envelope) error {
			// history_turns pages are turn-atomic. An overlapping turn could
			// otherwise return just a message suffix or invent a fallback ID.
			turn := value(event.TurnId)
			if turn != "" && readTurns[turn] {
				return errors.New("native Worker history split a turn across boundaries")
			}
			if turn != "" {
				pageTurns[turn] = true
			}
			key := value(event.ProjectionId)
			if key == "" {
				key = value(event.EventId)
			}
			if key != "" {
				if eventKeys[key] {
					return nil
				}
				eventKeys[key] = true
			}
			return projection.apply(event)
		}
		path := "/sessions/" + idPath(sid) + "/reconnect?history_turns=" + strconv.Itoa(64)
		if before != "" {
			path += "&history_before=" + url.QueryEscape(before)
		}
		bootstrap, synced, bytesRead, next := false, false, 0, ""
		snapshot, page, replaced, appended := "", 0, false, false
		err := c.stream(ctx, path, "", func(f frame) error {
			bytesRead += len(f.data)
			if bytesRead > historyPageBytes {
				return errors.New("native Worker history page exceeds the read limit")
			}
			if !bootstrap {
				var state wire.SessionState
				if f.event != "caelis.control.bootstrap" || json.Unmarshal(f.data, &state) != nil || state.SessionId != sid || state.ProtocolVersion != 1 || state.ApiVersion != "v1" || state.EnvelopeVersion != "caelis.control.envelope/v1" {
					return errors.New("original Worker history bootstrap is invalid")
				}
				bootstrap = true
				return nil
			}
			var delivery wire.SessionFeedDelivery
			// Exact history pages carry continuation cursors before the final
			// sync. Only sync carries the boundary for older history traversal.
			if f.event != "caelis.control.delivery" || json.Unmarshal(f.data, &delivery) != nil || f.id != "" && f.id != value(delivery.NextCursor) || delivery.Kind != "sync" && delivery.HistoryBefore != nil || delivery.Kind != "sync" && delivery.Kind != "append_page" && value(delivery.NextCursor) != "" {
				return errors.New("original Worker history delivery is invalid")
			}
			switch delivery.Kind {
			case "replace_begin":
				if snapshot != "" || replaced || appended || delivery.Source != "replacement" || value(delivery.SnapshotId) == "" || value(delivery.Page) != 0 || len(delivery.Events) != 0 {
					return errors.New("original Worker history replacement is invalid")
				}
				snapshot = value(delivery.SnapshotId)
			case "replace_page":
				if snapshot == "" || replaced || delivery.Source != "replacement" || value(delivery.SnapshotId) != snapshot || value(delivery.Page) != page {
					return errors.New("original Worker history page is invalid")
				}
				page++
				for _, event := range delivery.Events {
					if err := apply(event); err != nil {
						return err
					}
				}
			case "replace_end":
				if snapshot == "" || replaced || delivery.Source != "replacement" || value(delivery.SnapshotId) != snapshot || value(delivery.Page) != page || len(delivery.Events) != 0 {
					return errors.New("original Worker history replacement did not finish")
				}
				replaced = true
			case "append_page":
				if snapshot != "" || delivery.Source != "exact" || value(delivery.SnapshotId) != "" || value(delivery.Page) != 0 {
					return errors.New("original Worker history append is invalid")
				}
				appended = true
				for _, event := range delivery.Events {
					if err := apply(event); err != nil {
						return err
					}
				}
			case "sync":
				if synced || snapshot != "" && !replaced || delivery.Source != "exact" || value(delivery.SnapshotId) != "" || len(delivery.Events) != 0 || value(delivery.HistoryBefore) == before && before != "" {
					return errors.New("original Worker history boundary is invalid")
				}
				synced = true
				next = value(delivery.HistoryBefore)
				return historyComplete
			default:
				return errors.New("original Worker history contains an unexpected delivery")
			}
			return nil
		})
		if !errors.Is(err, historyComplete) || !bootstrap || !synced {
			return nil, errors.New("original native Worker history is unreadable; no empty result was inferred")
		}
		s.mu.Lock()
		current, exists := s.state.Workers[id]
		same := exists && s.client == c && s.connected && !s.closed && s.generation == generation && s.state.InstanceID == instance && sameWorkerOwner(w, current)
		s.mu.Unlock()
		if !same {
			return nil, errors.New("original native Worker changed during history read")
		}
		for turn := range pageTurns {
			readTurns[turn] = true
		}
		if next != "" {
			if cursors[next] {
				return nil, errors.New("native Worker history repeated its cursor")
			}
			cursors[next] = true
		}
		var older []api.TaskConversationMessage
		for _, item := range projection.completed() {
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			older = append(older, item)
		}
		ordered = append(older, ordered...)
		if next == "" {
			if ordered == nil {
				return []api.TaskConversationMessage{}, nil
			}
			return ordered, nil
		}
		before = next
	}
	return nil, errors.New("native Worker history exceeds the bounded page traversal")
}

var _ api.WorkConversationReader = (*Session)(nil)
