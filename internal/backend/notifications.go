package backend

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
)

// NotificationObserver consumes native facts, not prose, and is called by one
// host observer. Restored completed history must not announce itself again.
type NotificationObserver struct {
	Locale             func() i18n.Locale
	SkipResults        bool
	initialized        bool
	lastUser, terminal string
	scheduledTurn      string
	decisions          map[string]bool
	Notify             func(id, title, body string, reminder bool)
}

func (o *NotificationObserver) Observe(v api.Snapshot) {
	if v.Connection != "ready" || o.Notify == nil {
		return
	}
	if o.decisions == nil {
		o.decisions = map[string]bool{}
	}
	locale := i18n.English
	if o.Locale != nil {
		locale = o.Locale()
	}
	text := func(key string) string { return i18n.Text(locale, "host."+key, nil) }
	user := ""
	for _, i := range v.Items {
		if i.Kind == "user" {
			user = i.ID
		}
	}
	if !o.initialized {
		o.initialized = true
		o.lastUser = user
		if v.Scheduled && !v.CanInterrupt && (v.Phase == "completed" || v.Phase == "failed") {
			o.scheduledTurn = v.CurrentTurn
		}
	}
	for _, a := range v.Approvals {
		if a.Status == "pending" && !o.decisions[a.ID] {
			o.decisions[a.ID] = true
			o.Notify("approval-"+a.ID, text("approvalTitle"), text("approvalBody"), false)
		}
	}
	if v.Scheduled {
		if o.SkipResults || v.Quiet || v.CanInterrupt || (v.Phase != "completed" && v.Phase != "failed") || v.CurrentTurn == "" || o.scheduledTurn == v.CurrentTurn {
			return
		}
		o.scheduledTurn = v.CurrentTurn
		title := text("scheduledTitle")
		if v.Phase == "failed" {
			title = text("scheduledFailed")
		}
		o.Notify("scheduled-"+v.CurrentTurn, title, text("replyBody"), true)
		return
	}
	if o.SkipResults || v.CanInterrupt || (v.Phase != "completed" && v.Phase != "failed") || user == "" || user == o.lastUser || user == o.terminal {
		return
	}
	o.terminal = user
	title := text("resultTitle")
	if v.Phase == "failed" {
		title = text("resultFailed")
	}
	o.Notify("result-"+user, title, text("replyBody"), false)
}
