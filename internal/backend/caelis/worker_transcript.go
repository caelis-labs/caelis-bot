package caelis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// The conversation reader uses native messageId when present. The ordinary
// chat projection groups text by turn for display, which would merge two
// completed assistant messages in one Worker turn.
type workerTranscript struct {
	session  string
	items    []api.TaskConversationMessage
	index    map[string]int
	active   map[string]string
	complete map[string]bool
}

func newWorkerTranscript(session string) *workerTranscript {
	return &workerTranscript{session: session, index: map[string]int{}, active: map[string]string{}, complete: map[string]bool{}}
}

func nativeWorkerMessageID(parts ...string) string {
	encoded, _ := json.Marshal(parts)
	sum := sha256.Sum256(encoded)
	return "msg-" + hex.EncodeToString(sum[:16])
}

func (p *workerTranscript) finish(turn string) {
	key := turn + "\x00assistant"
	if id := p.active[key]; id != "" {
		p.complete[id] = true
		delete(p.active, key)
	}
}

func (p *workerTranscript) apply(e wire.Envelope) error {
	if value(e.Scope) != "" && value(e.Scope) != "main" || value(e.ApprovalRequestId) != "" {
		return nil
	}
	turn := value(e.TurnId)
	if e.Kind == "caelis/lifecycle" && e.Lifecycle != nil && e.Lifecycle.State == "completed" {
		p.finish(turn)
	}
	if e.Update == nil {
		return nil
	}
	var update struct {
		Kind        string          `json:"sessionUpdate"`
		MessageID   string          `json:"messageId"`
		Content     json.RawMessage `json:"content"`
		Replacement bool            `json:"replacement"`
	}
	if err := json.Unmarshal(*e.Update, &update); err != nil {
		return errors.New("native Worker message update is invalid")
	}
	if update.Kind == "agent_thought_chunk" || update.Kind == "tool_call" {
		p.finish(turn)
		return nil
	}
	role := ""
	switch update.Kind {
	case "user_message_chunk":
		if e.AgentCommunicationSource != nil {
			return nil
		}
		role = "user"
	case "agent_message_chunk":
		role = "assistant"
	default:
		return nil
	}
	var content struct{ Type, Text string }
	if json.Unmarshal(update.Content, &content) != nil || content.Type != "text" || content.Text == "" && !update.Replacement || turn == "" {
		return nil
	}
	activeKey := turn + "\x00" + role
	id := ""
	if update.MessageID != "" {
		id = nativeWorkerMessageID(p.session, turn, role, update.MessageID)
	} else {
		id = p.active[activeKey]
		if id == "" {
			if value(e.EventId) == "" {
				return errors.New("native Worker message identity is missing")
			}
			id = nativeWorkerMessageID(p.session, turn, role, value(e.EventId))
		}
	}
	if index, exists := p.index[id]; exists {
		if update.Replacement {
			p.items[index].Text = content.Text
		} else {
			p.items[index].Text += content.Text
		}
	} else {
		p.index[id] = len(p.items)
		p.items = append(p.items, api.TaskConversationMessage{ID: id, Role: role, Text: content.Text})
	}
	p.active[activeKey] = id
	if role == "user" {
		p.complete[id] = true // A native user update records accepted input.
	}
	if value(e.Final) {
		p.complete[id] = true
		delete(p.active, activeKey)
	}
	return nil
}

func (p *workerTranscript) completed() []api.TaskConversationMessage {
	out := make([]api.TaskConversationMessage, 0, len(p.items))
	for _, item := range p.items {
		if p.complete[item.ID] {
			out = append(out, item)
		}
	}
	return out
}
