package caelis

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (s *Session) requestRefreshLocked() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Unrelated output must not starve approval delivery. Conversely a newer
// approval event or a replaced view fences an in-flight state read.
func reconcileApproval(v, before *view, version uint64, state wire.SessionState) {
	if v == before && v.ApprovalVersion == version || before == nil && v.ApprovalVersion == 0 {
		v.State.Approval = state.Approval
		v.ApprovalDirty = false
	}
}

// The pinned Host's SessionState.permission is the durable Control shape,
// not RequestPermission's ACP event shape. Keep the original object in the
// approval identity; these fields are only used for presentation and choices.
type nativePermission struct {
	ToolCall struct {
		ID       string          `json:"id"`
		Kind     string          `json:"kind"`
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
