package codex

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
)

// ReadWorkConversation pages the original native thread in FIFO order. The
// resident chat projection and completion notification are not consumed.
func (s *Session) ReadWorkConversation(ctx context.Context, id string) ([]api.TaskConversationMessage, error) {
	ctx, cancel := s.operation(ctx, 30*time.Second)
	defer cancel()
	if err := lockwait.Lock(ctx, &s.op); err != nil {
		return nil, err
	}
	defer s.op.Unlock()
	s.mu.Lock()
	t := s.binding.Tasks[id]
	c := s.client
	ready := s.state.Connection == "ready"
	epoch := s.epoch
	archived := s.binding.LostOwnerTasks[id]
	thread := ""
	retainedRun := ""
	completed := map[string]bool{}
	if t != nil {
		thread = t.Thread
		retainedRun = t.Run
		for id := range t.CompletedMessages {
			completed[id] = true
		}
	}
	s.mu.Unlock()
	if t == nil {
		return nil, errors.New("only Bot-created tasks can be read")
	}
	if thread == "" || c == nil || !ready {
		return nil, errors.New("original native Worker transcript is not readable; retain the task handle and retry after reconnect")
	}
	var messages []api.TaskConversationMessage
	seen := map[string]bool{}
	turns := map[string]bool{}
	cursors := map[string]bool{}
	cursor := ""
	for pageNumber := 0; pageNumber < 1000; pageNumber++ {
		params := map[string]any{"threadId": thread, "limit": 20, "sortDirection": "asc", "itemsView": "full"}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page turnPage
		if err := callDecode(ctx, c, "thread/turns/list", params, &page); err != nil {
			return nil, err
		}
		s.mu.Lock()
		same := s.client == c && s.epoch == epoch && !s.closed && s.state.Connection == "ready" &&
			s.binding.Tasks[id] == t && t.Thread == thread && t.Run == retainedRun && s.binding.LostOwnerTasks[id] == archived
		s.mu.Unlock()
		if !same {
			return nil, errors.New("original native Worker changed during history read")
		}
		if page.Data == nil || len(page.Data) > 20 || len(page.Data) == 0 && page.NextCursor != "" {
			return nil, ErrProtocol
		}
		for _, turn := range page.Data {
			if turn.ID == "" {
				return nil, ErrProtocol
			}
			turns[turn.ID] = true
			for index, item := range turn.Items {
				role := ""
				switch item.Type {
				case "userMessage":
					role = "user"
				case "agentMessage":
					role = "assistant"
				default:
					continue
				}
				if item.ID == "" {
					return nil, ErrProtocol
				}
				// Native agentMessage has no status field. A later native item
				// (except a steering user message) proves the earlier output
				// finished. A terminal turn proves its final item finished.
				// The live item/completed receipt also covers a completed
				// progress message while the turn is still running.
				completedInHistory := index+1 < len(turn.Items) && turn.Items[index+1].Type != "userMessage" ||
					turn.Status == "completed" && index == len(turn.Items)-1
				if role == "assistant" && !completed[opaque(turn.ID, item.ID)] && !completedInHistory {
					continue
				}
				text := item.Text
				if role == "user" {
					parts := make([]string, 0, len(item.Content))
					for _, content := range item.Content {
						if content.Type == "text" && content.Text != "" {
							parts = append(parts, content.Text)
						}
					}
					text = strings.Join(parts, "\n")
				}
				key := turn.ID + "\x00" + item.ID
				if seen[key] {
					continue
				}
				seen[key] = true
				messages = append(messages, api.TaskConversationMessage{ID: "msg-" + opaque(thread, turn.ID, item.ID), Role: role, Text: text})
			}
		}
		if page.NextCursor == "" {
			if archived && (len(messages) == 0 || retainedRun != "" && !turns[retainedRun]) {
				return nil, errors.New("original archived Worker messages are unavailable; retain the task and its receipts")
			}
			if messages == nil {
				return []api.TaskConversationMessage{}, nil
			}
			return messages, nil
		}
		if page.NextCursor == cursor || cursors[page.NextCursor] {
			return nil, ErrProtocol
		}
		cursors[page.NextCursor] = true
		cursor = page.NextCursor
	}
	return nil, errors.New("native Worker transcript exceeds the bounded page traversal")
}

var _ api.WorkConversationReader = (*Session)(nil)
