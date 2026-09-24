package codex

import "encoding/json"

// ThreadItem is a tagged union. A reasoning item's content is []string, while
// userMessage content is []UserInput. Decode only fields owned by the selected
// variant, including on history/reconnect and in turn completion payloads.
func (item *nativeItem) UnmarshalJSON(data []byte) error {
	var header struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	fields := []string{"id", "type"}
	switch header.Type {
	case "userMessage":
		fields = append(fields, "clientId", "content")
	case "agentMessage":
		fields = append(fields, "text", "phase", "questions")
	case "plan":
		fields = append(fields, "text")
	case "commandExecution":
		fields = append(fields, "status", "command", "cwd", "aggregatedOutput", "exitCode")
	case "fileChange":
		fields = append(fields, "status", "changes")
	case "mcpToolCall", "dynamicToolCall":
		fields = append(fields, "status", "server", "tool", "arguments", "result", "error")
	case "collabAgentToolCall":
		fields = append(fields, "status", "tool", "receiverThreadIds")
	case "subAgentActivity":
		fields = append(fields, "agentThreadId", "kind")
	case "imageView", "imageGeneration":
		fields = append(fields, "status", "path")
	default:
		*item = nativeItem{ID: header.ID, Type: header.Type}
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	selected := make(map[string]json.RawMessage, len(fields))
	for _, field := range fields {
		if value, ok := raw[field]; ok {
			selected[field] = value
		}
	}
	b, err := json.Marshal(selected)
	if err != nil {
		return err
	}
	type wireItem nativeItem
	var decoded wireItem
	if err := json.Unmarshal(b, &decoded); err != nil {
		return err
	}
	*item = nativeItem(decoded)
	return nil
}
