package backend

import (
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Outgoing messages are presentation only. Native receipts and correlated input
// events own acceptance; losing a renderer never resubmits an uncertain command.
type outgoingMessage struct {
	item  api.Item
	after string
}

func (s *Service) stageOutgoing(input api.Submission, files []api.InputFile) {
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
			return
		}
	}
	after := ""
	if len(v.Items) > 0 {
		after = v.Items[len(v.Items)-1].ID
	}
	s.outbox = append(s.outbox, outgoingMessage{item: api.Item{ID: "outgoing:" + input.ID, RequestID: input.ID, Kind: "user", Text: strings.TrimSpace(text), Status: "sending", Artifacts: []api.Artifact{}}, after: after})
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
		}
	}
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
			continue
		}
		if v.LastReceipt.ID == pending.item.RequestID && v.LastReceipt.Outcome != "" {
			pending.item.Status = v.LastReceipt.Outcome
		}
		remaining = append(remaining, pending)
		at := len(items)
		if pending.after == "" {
			at = 0
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
	s.outbox = remaining
	v.Items = items
	return v
}
