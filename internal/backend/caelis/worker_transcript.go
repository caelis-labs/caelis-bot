package caelis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// The conversation reader uses native messageId when present. The ordinary
// chat projection groups text by turn for display, which would merge two
// completed assistant messages in one Worker turn.
type workerTranscript struct {
	Session  string                        `json:"session"`
	Items    []api.TaskConversationMessage `json:"items"`
	Active   map[string]string             `json:"active,omitempty"`
	Complete map[string]bool               `json:"complete,omitempty"`
	Turns    map[string]string             `json:"turns,omitempty"`
	index    map[string]int
}

func newWorkerTranscript(session string) *workerTranscript {
	return &workerTranscript{Session: session, index: map[string]int{}, Active: map[string]string{}, Complete: map[string]bool{}, Turns: map[string]string{}}
}

func nativeWorkerMessageID(parts ...string) string {
	encoded, _ := json.Marshal(parts)
	sum := sha256.Sum256(encoded)
	return "msg-" + hex.EncodeToString(sum[:16])
}

func (p *workerTranscript) finish(turn string) {
	key := turn + "\x00assistant"
	if id := p.Active[key]; id != "" {
		p.Complete[id] = true
		delete(p.Active, key)
	}
}

func (p *workerTranscript) apply(e wire.Envelope) error {
	p.initialize()
	if e.SessionId != nil && value(e.SessionId) != p.Session {
		return errors.New("native Worker history belongs to another Session")
	}
	if value(e.Scope) != "" && value(e.Scope) != "main" || value(e.ApprovalRequestId) != "" {
		return nil
	}
	turn := value(e.TurnId)
	if e.Kind == "caelis/lifecycle" && e.Lifecycle != nil {
		switch e.Lifecycle.State {
		case "completed", "failed", "cancelled", "interrupted", "stopped":
			p.finish(turn)
			for _, item := range p.Items {
				if item.Role == "assistant" && p.Turns[item.ID] == turn {
					p.Complete[item.ID] = true
				}
			}
		}
	}
	if e.Update == nil {
		return nil
	}
	var update struct {
		Kind      string          `json:"sessionUpdate"`
		MessageID string          `json:"messageId"`
		Content   json.RawMessage `json:"content"`
		Name      string          `json:"name"`
		Status    string          `json:"status"`
		RawOutput json.RawMessage `json:"rawOutput"`
	}
	if err := json.Unmarshal(*e.Update, &update); err != nil {
		return errors.New("native Worker message update is invalid")
	}
	if update.Kind == "tool_call_update" && update.Name == "PublishArtifact" && update.Status == "completed" {
		var raw struct {
			Result string `json:"result"`
		}
		var output struct {
			Resource wire.SessionResource `json:"resource"`
		}
		if json.Unmarshal(update.RawOutput, &raw) == nil && json.Unmarshal([]byte(raw.Result), &output) == nil && output.Resource.SessionId == p.Session && output.Resource.Id != "" {
			id := "resource:" + p.Session + ":" + output.Resource.Id
			if _, exists := p.index[id]; !exists {
				name := filepath.Base(strings.ReplaceAll(output.Resource.Name, "\\", "/"))
				if name == "." || name == ".." || name == "" {
					name = "artifact"
				}
				p.index[id] = len(p.Items)
				p.Items = append(p.Items, api.TaskConversationMessage{ID: id, Role: "assistant", Artifacts: []api.Artifact{{ID: id, Name: name}}})
				p.Complete[id] = true
				p.Turns[id] = turn
			}
		}
		return nil
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
	if json.Unmarshal(update.Content, &content) != nil || content.Type != "text" || content.Text == "" || turn == "" {
		return nil
	}
	activeKey := turn + "\x00" + role
	id := ""
	if update.MessageID != "" {
		id = nativeWorkerMessageID(p.Session, turn, role, update.MessageID)
	} else {
		id = p.Active[activeKey]
		if id == "" {
			if value(e.EventId) == "" {
				return errors.New("native Worker message identity is missing")
			}
			id = nativeWorkerMessageID(p.Session, turn, role, value(e.EventId))
		}
	}
	if index, exists := p.index[id]; exists {
		p.Items[index].Text += content.Text
	} else {
		p.index[id] = len(p.Items)
		p.Items = append(p.Items, api.TaskConversationMessage{ID: id, Role: role, Text: content.Text})
	}
	p.Active[activeKey] = id
	p.Turns[id] = turn
	if role == "user" {
		p.Complete[id] = true // A native user update records accepted input.
	}
	if value(e.Final) {
		p.Complete[id] = true
		delete(p.Active, activeKey)
	}
	return nil
}

func (p *workerTranscript) completed() []api.TaskConversationMessage {
	out := make([]api.TaskConversationMessage, 0, len(p.Items))
	for _, item := range p.Items {
		if p.Complete[item.ID] {
			out = append(out, item)
		}
	}
	return out
}

// A Worker view retains a bounded current projection, not an event log. The
// native history reader uses this same segmenter without this display bound.
func (p *workerTranscript) initialize() {
	if p.index == nil {
		p.index = map[string]int{}
		for i, item := range p.Items {
			p.index[item.ID] = i
		}
	}
	if p.Active == nil {
		p.Active = map[string]string{}
	}
	if p.Complete == nil {
		p.Complete = map[string]bool{}
	}
	if p.Turns == nil {
		p.Turns = map[string]string{}
	}
}
func (p *workerTranscript) bound() {
	p.initialize()
	used, first := 0, len(p.Items)
	for i := len(p.Items) - 1; i >= 0 && len(p.Items)-i <= 200; i-- {
		item := &p.Items[i]
		item.Text = workSnapshotTail(item.Text, 128<<10)
		cost := len(item.Text) + len(item.ID) + len(item.Role) + len(p.Turns[item.ID])
		for _, a := range item.Artifacts {
			cost += len(a.ID) + len(a.Name)
		}
		if used+cost > 512<<10 {
			break
		}
		used += cost
		first = i
	}
	if first > 0 {
		p.Items = append([]api.TaskConversationMessage(nil), p.Items[first:]...)
	}
	p.index = map[string]int{}
	for i, item := range p.Items {
		p.index[item.ID] = i
	}
	for id := range p.Complete {
		if _, ok := p.index[id]; !ok {
			delete(p.Complete, id)
		}
	}
	for id := range p.Turns {
		if _, ok := p.index[id]; !ok {
			delete(p.Turns, id)
		}
	}
	for key, id := range p.Active {
		if _, ok := p.index[id]; !ok {
			delete(p.Active, key)
		}
	}
}
func (p *workerTranscript) current() []api.Item {
	p.initialize()
	out := make([]api.Item, 0, len(p.Items))
	for _, message := range p.Items {
		status := "inProgress"
		if p.Complete[message.ID] {
			status = "completed"
		}
		out = append(out, api.Item{ID: "worker-live:" + message.ID, HistoryID: message.ID, TurnKey: p.Turns[message.ID], Kind: message.Role, Text: message.Text, Status: status, Artifacts: clone(message.Artifacts)})
	}
	return out
}

// Replacement history may omit already observed older messages. Keep those
// exact IDs, while the new canonical page wins for any ID it does contain.
func (p *workerTranscript) retainMissing(old *workerTranscript) {
	if old == nil || old.Session != p.Session {
		return
	}
	p.initialize()
	old.initialize()
	prefix := []api.TaskConversationMessage{}
	for _, item := range old.Items {
		if index, exists := p.index[item.ID]; exists {
			// A partial replay cannot undo an already observed native final.
			if old.Complete[item.ID] && !p.Complete[item.ID] {
				p.Items[index] = clone(item)
				p.Complete[item.ID], p.Turns[item.ID] = true, old.Turns[item.ID]
			}
			continue
		}
		prefix = append(prefix, clone(item))
		p.Complete[item.ID], p.Turns[item.ID] = old.Complete[item.ID], old.Turns[item.ID]
	}
	p.Items = append(prefix, p.Items...)
	for key, id := range old.Active {
		if _, exists := p.Active[key]; !exists {
			p.Active[key] = id
		}
	}
	for key, id := range p.Active {
		if p.Complete[id] {
			delete(p.Active, key)
		}
	}
	p.bound()
}
