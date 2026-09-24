package backend

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

// NotificationObserver consumes native facts, not prose, and is called by one
// host observer. Restored completed history must not announce itself again.
type NotificationObserver struct {
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
			o.Notify("approval-"+a.ID, "需要你的确认", "点击打开 Caelis Bot，查看操作并作出决定。", false)
		}
	}
	if v.Scheduled {
		if o.SkipResults || v.Quiet || v.CanInterrupt || (v.Phase != "completed" && v.Phase != "failed") || v.CurrentTurn == "" || o.scheduledTurn == v.CurrentTurn {
			return
		}
		o.scheduledTurn = v.CurrentTurn
		title := "定时任务有新消息"
		if v.Phase == "failed" {
			title = "定时任务未能完成"
		}
		o.Notify("scheduled-"+v.CurrentTurn, title, "点击查看 Caelis Bot 的回复。", true)
		return
	}
	if o.SkipResults || v.CanInterrupt || (v.Phase != "completed" && v.Phase != "failed") || user == "" || user == o.lastUser || user == o.terminal {
		return
	}
	o.terminal = user
	title := "工作已完成"
	if v.Phase == "failed" {
		title = "工作未能完成"
	}
	o.Notify("result-"+user, title, "点击查看 Caelis Bot 的回复。", false)
}
