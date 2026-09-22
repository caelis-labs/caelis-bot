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
