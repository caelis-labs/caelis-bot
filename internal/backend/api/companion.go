package api

import (
	"context"
	"encoding/json"
)

// DesktopEffects is native-only. The provider must claim a Control action before
// calling Execute; the renderer cannot create claims, sources or reminder grants.
type DesktopEffects struct {
	Execute func(action string, arguments json.RawMessage) (json.RawMessage, error)
	Notify  func(id, title, body string, reminder bool)
}

// ControlCompanion owns delegation/report admission remotely, and never receives
// a second local MCP tool server or the Codex scheduler's user-prompt wakeups.
type ControlCompanion interface {
	BindDesktop(DesktopEffects) error
	OwnedTasks(context.Context) ([]Task, error)
}

// PresentationAcknowledger consumes only the report represented by this exact
// product snapshot. Reading a task or delivering an OS notification is not ack.
type PresentationAcknowledger interface {
	AcknowledgePresentation(context.Context, Snapshot) error
}
