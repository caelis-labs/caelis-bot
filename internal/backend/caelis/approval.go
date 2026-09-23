package caelis

import "encoding/json"

// The pinned Host's SessionState.permission is the durable Control shape,
// not RequestPermission's ACP event shape. Keep the original object in the
// approval identity; these fields are only used for presentation and choices.
type nativePermission struct {
	ToolCall struct {
		Title    string          `json:"title"`
		RawInput json.RawMessage `json:"raw_input"`
	} `json:"tool_call"`
	Options []nativeOption `json:"options"`
}
type nativeOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}
