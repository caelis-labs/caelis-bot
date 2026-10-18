package backend

import (
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
)

// NotificationObserver announces decisions and meaningful scheduled messages.
// Ordinary replies belong to chat/the pet bubble, never a second OS alert.
// Restored history and technical recovery states are not notification events.
// The native host applies foreground suppression; this observer runs serially.
type NotificationObserver struct {
	Locale        func() i18n.Locale
	Notify        func(id, title, body string, reminder bool)
	Dismiss       func(id string)
	initialized   bool
	clearedLegacy bool
	scheduledTurn string
	decisions     map[string]bool
	pending       map[string]bool
}

func (o *NotificationObserver) Observe(v api.Snapshot) {
	if o.Notify == nil {
		return
	}
	if !o.clearedLegacy {
		// Remove the obsolete recovery toast even if the connection is offline.
		if o.Dismiss != nil {
			o.Dismiss("runtime-recovery")
		}
		o.clearedLegacy = true
	}
	if v.Connection != "ready" {
		return // A lost connection is not evidence that a decision was resolved.
	}
	if !o.initialized {
		o.initialized = true
		o.decisions = map[string]bool{}
		if v.Scheduled && !v.CanInterrupt && (v.Phase == "completed" || v.Phase == "failed") {
			o.scheduledTurn = v.CurrentTurn
		}
	}
	locale := i18n.English
	if o.Locale != nil {
		locale = o.Locale()
	}
	text := func(key string) string { return i18n.Text(locale, "host."+key, nil) }
	pending := map[string]bool{}
	for _, a := range v.Approvals {
		if a.Status != "pending" || a.ID == "" {
			continue
		}
		pending[a.ID] = true
		if !o.decisions[a.ID] {
			o.decisions[a.ID] = true
			// Do not copy command arguments, credentials or private approval payloads
			// to the lock screen. The original decision stays in the app.
			o.Notify("approval-"+a.ID, text("approvalTitle"), text("approvalBody"), false)
		}
	}
	for id := range o.pending {
		if !pending[id] && o.Dismiss != nil {
			o.Dismiss("approval-" + id)
		}
	}
	o.pending = pending
	if !v.Scheduled || v.Quiet || v.CanInterrupt || v.Phase != "completed" || v.CurrentTurn == "" || o.scheduledTurn == v.CurrentTurn {
		return
	}
	// Only current-turn assistant content can become a reminder preview. Never
	// fall back to an earlier reply, a tool result or a generic completion toast.
	for n := len(v.Items) - 1; n >= 0; n-- {
		i := v.Items[n]
		if i.Kind != "assistant" || i.TurnKey != v.CurrentTurn || strings.TrimSpace(i.Text) == "" {
			continue
		}
		if strings.TrimSpace(i.Text) == api.SilentReminder {
			return
		}
		body := notificationPreview(i.Text)
		if body != "" {
			o.scheduledTurn = v.CurrentTurn
			o.Notify("scheduled-"+v.CurrentTurn, text("scheduledTitle"), body, true)
		}
		return
	}
}
