package backend

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestNativeNotificationObserverIgnoresRestoredHistoryAndDeduplicates(t *testing.T) {
	var delivered []string
	o := NotificationObserver{Notify: func(id, title, body string, _ bool) { delivered = append(delivered, id) }}
	v := api.Snapshot{Connection: "ready", Phase: "completed", Items: []api.Item{{ID: "old", Kind: "user", Text: "private"}}}
	o.Observe(v)
	if len(delivered) != 0 {
		t.Fatal("notified restored history")
	}
	v.Items = append(v.Items, api.Item{ID: "new", Kind: "user"})
	v.Phase = "working"
	v.CanInterrupt = true
	v.Approvals = []api.Approval{{ID: "decision", Status: "pending"}}
	o.Observe(v)
	o.Observe(v)
	v.Approvals = nil
	v.Phase = "completed"
	v.CanInterrupt = false
	o.Observe(v)
	o.Observe(v)
	if len(delivered) != 2 || delivered[0] != "approval-decision" || delivered[1] != "result-new" {
		t.Fatal(delivered)
	}
}

func TestScheduledNotificationsOnlyAnnounceVisibleResultOnce(t *testing.T) {
	var ids []string
	o := NotificationObserver{Notify: func(id, title, body string, reminder bool) {
		if !reminder {
			t.Error("scheduled result must reach a visible pet")
		}
		ids = append(ids, id)
	}}
	o.Observe(api.Snapshot{Connection: "ready", Phase: "idle"})
	v := api.Snapshot{Connection: "ready", Phase: "working", CurrentTurn: "one", Scheduled: true, Quiet: true, CanInterrupt: true}
	o.Observe(v)
	v.Phase = "completed"
	v.CanInterrupt = false
	o.Observe(v)
	if len(ids) != 0 {
		t.Fatal("silent task notified", ids)
	}
	v.CurrentTurn = "two"
	v.Quiet = false
	v.Items = []api.Item{{ID: "reply", Kind: "assistant", TurnKey: "two", Text: "water"}}
	o.Observe(v)
	o.Observe(v)
	if len(ids) != 1 || ids[0] != "scheduled-two" {
		t.Fatal(ids)
	}
	restored := NotificationObserver{Notify: o.Notify}
	restored.Observe(v)
	if len(ids) != 1 {
		t.Fatal("replayed a restored result")
	}
	v.CurrentTurn = "three"
	v.Phase = "failed"
	v.Items = nil
	o.Observe(v)
	if len(ids) != 2 {
		t.Fatal("failure hidden")
	}
}

func TestScheduledNotificationAfterReconnectDuringExecution(t *testing.T) {
	var delivered int
	o := NotificationObserver{Notify: func(string, string, string, bool) { delivered++ }}
	v := api.Snapshot{Connection: "ready", Phase: "working", CurrentTurn: "ongoing", Scheduled: true, Quiet: true, CanInterrupt: true}
	o.Observe(v)
	v.Phase = "completed"
	v.CanInterrupt = false
	v.Quiet = false
	o.Observe(v)
	o.Observe(v)
	if delivered != 1 {
		t.Fatalf("completion after reconnect: %d notifications", delivered)
	}
}
