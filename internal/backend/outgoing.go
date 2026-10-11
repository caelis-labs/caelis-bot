package backend

import (
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Outgoing messages are presentation only. Native receipts and correlated input
// events own acceptance; losing a renderer never resubmits an uncertain command.
type outgoingMessage struct {
	item  api.Item
	after string
}

func (s *Service) stageOutgoing(input api.Submission, files []api.InputFile) bool {
	v := s.engine.Snapshot()
	text := input.Text
	for _, f := range files {
		text += "\n" + f.Name
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v = s.presentOutgoing(v)
	for _, pending := range s.outbox {
		if pending.item.RequestID == input.ID {
			return false
		}
	}
	after := ""
	if len(v.Items) > 0 {
		after = v.Items[len(v.Items)-1].ID
	}
	item := api.Item{ID: "outgoing:" + input.ID, RequestID: input.ID, Kind: "user", Text: strings.TrimSpace(text), Status: "sending", SeenAt: time.Now().UnixMicro(), Artifacts: []api.Artifact{}}
	if !input.ScreenInput {
		item.Media = s.messageMedia.Presentation(input.ID)
	}
	s.outbox = append(s.outbox, outgoingMessage{item: item, after: after})
	s.presentationRevision++
	return true
}

func (s *Service) finishOutgoing(id string, receipt api.Receipt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.outbox {
		if s.outbox[i].item.RequestID == id {
			status := receipt.Outcome
			if status == "" {
				status = "unknown"
			}
			s.outbox[i].item.Status = status
			s.presentationRevision++
			_ = s.messageMedia.Mark(id, s.outbox[i].item.Text, status)
		}
	}
}

// Persist the user's original display text once the native receipt is known.
// A later native echo updates this same request ID instead of creating a second
// chat message. The quoted model input and internal plugin hints stay out of it.
func (s *Service) recordInput(input api.Submission, files []api.InputFile, receipt api.Receipt) {
	if s.chat == nil || input.ID == "" || receipt.Outcome != "accepted" && receipt.Outcome != "rejected" && receipt.Outcome != "unknown" {
		return
	}
	text := input.Text
	for _, f := range files {
		text += "\n" + f.Name
	}
	item := api.Item{ID: "outgoing:" + input.ID, RequestID: input.ID, Kind: "user", Text: strings.TrimSpace(text), Status: receipt.Outcome}
	s.mu.Lock()
	for _, pending := range s.outbox {
		if pending.item.RequestID == input.ID {
			item.SeenAt = pending.item.SeenAt
			break
		}
	}
	s.mu.Unlock()
	if !input.ScreenInput {
		item.Media = s.messageMedia.Presentation(input.ID)
	}
	s.chat.Observe([]api.Item{item})
}

// A proven pre-dispatch recovery refusal must leave no local sending bubble.
func (s *Service) discardOutgoing(id string) {
	s.mu.Lock()
	for i := 0; i < len(s.outbox); i++ {
		if s.outbox[i].item.RequestID == id {
			s.outbox = append(s.outbox[:i], s.outbox[i+1:]...)
			s.presentationRevision++
			break
		}
	}
	s.mu.Unlock()
	s.messageMedia.Resolve(id)
}

// Called under mu. Match request identities, never message text (two identical
// messages can be two separate prompts or safe-point inputs in the same turn).
func (s *Service) presentOutgoing(v api.Snapshot) api.Snapshot {
	if len(s.outbox) == 0 {
		return v
	}
	items := append([]api.Item(nil), v.Items...)
	remaining := s.outbox[:0]
	for _, pending := range s.outbox {
		confirmed := false
		for _, item := range v.Items {
			if item.Kind == "user" && item.RequestID == pending.item.RequestID {
				confirmed = true
				break
			}
		}
		if confirmed {
			s.messageMedia.Resolve(pending.item.RequestID)
			continue
		}
		if v.LastReceipt.ID == pending.item.RequestID && v.LastReceipt.Outcome != "" {
			pending.item.Status = v.LastReceipt.Outcome
		}
		// A terminal local bubble may remain beside its original anchor, but
		// a bounded history replacement must not append it as new input.
		if pending.item.Status == "accepted" || pending.item.Status == "rejected" {
			anchored := false
			for _, item := range v.Items {
				if item.ID == pending.after || item.RequestID != "" && "outgoing:"+item.RequestID == pending.after {
					anchored = true
					break
				}
			}
			if pending.after == "" {
				anchored = len(v.Items) == 0
			}
			if !anchored {
				continue
			}
		}
		remaining = append(remaining, pending)
		at := len(items)
		if pending.after == "" {
			at = 0
		}
		if pending.after == restoredOutgoingTail {
			at = len(items)
		}
		for i, item := range items {
			if item.ID == pending.after || item.RequestID != "" && "outgoing:"+item.RequestID == pending.after {
				at = i + 1
				break
			}
		}
		items = append(items, api.Item{})
		copy(items[at+1:], items[at:])
		items[at] = pending.item
	}
	if len(remaining) != len(s.outbox) {
		s.presentationRevision++
	}
	s.outbox = remaining
	v.Items = items
	return v
}
