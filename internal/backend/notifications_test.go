package backend

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"strings"
	"testing"
	"unicode/utf8"
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
	if len(delivered) != 1 || delivered[0] != "approval-decision" {
		t.Fatal(delivered)
	}
}

func TestRecoveryIsSilentAndClearsLegacyNoticeOnce(t *testing.T) {
	var sent, cleared []string
	o := NotificationObserver{Notify: func(id, _, _ string, _ bool) { sent = append(sent, id) }, Dismiss: func(id string) { cleared = append(cleared, id) }}
	v := api.Snapshot{Connection: "offline", Phase: "unknown", RecoveryNoticeKey: "original-request"}
	o.Observe(v)
	o.Observe(v)
	if len(sent) != 0 {
		t.Fatal("technical recovery escaped into OS notification", sent)
	}
	v.Connection, v.RecoveryNoticeKey, v.Phase = "ready", "", "completed"
	o.Observe(v)
	o.Observe(v)
	if len(cleared) != 1 || cleared[0] != "runtime-recovery" || len(sent) != 0 {
		t.Fatal("stale recovery notice survived", sent, cleared)
	}
}

func TestScheduledNotificationsOnlyAnnounceVisibleResultOnce(t *testing.T) {
	var ids []string
	o := NotificationObserver{Notify: func(id, title, body string, reminder bool) {
		if !reminder {
			t.Error("scheduled result must reach a visible pet")
		}
		if body != "water" {
			t.Errorf("reminder did not carry its content: %q", body)
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
	if len(ids) != 1 {
		t.Fatal("technical failure became a notification")
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
	v.Items = []api.Item{{Kind: "assistant", TurnKey: "ongoing", Text: "A useful reminder"}}
	o.Observe(v)
	o.Observe(v)
	if delivered != 1 {
		t.Fatalf("completion after reconnect: %d notifications", delivered)
	}
}

func TestOrdinaryRepliesNeverNotifyEvenWhenHistoryOrPhaseChanges(t *testing.T) {
	var delivered int
	o := NotificationObserver{Notify: func(string, string, string, bool) { delivered++ }}
	o.Observe(api.Snapshot{Connection: "ready", Phase: "idle"})
	for _, phase := range []string{"working", "completed", "failed", "unknown", "completed"} {
		o.Observe(api.Snapshot{Connection: "ready", Phase: phase, CurrentTurn: "new", Items: []api.Item{
			{ID: "user", Kind: "user", TurnKey: "new", Text: "Name yourself"},
			{ID: "reply", Kind: "assistant", TurnKey: "new", Text: "Remembered"},
		}})
	}
	if delivered != 0 {
		t.Fatal("ordinary reply generated an OS alert")
	}
}

func TestDecisionNotificationClearsOnlyAfterAuthoritativeResolution(t *testing.T) {
	var delivered, dismissed []string
	o := NotificationObserver{
		Notify:  func(id, _, _ string, _ bool) { delivered = append(delivered, id) },
		Dismiss: func(id string) { dismissed = append(dismissed, id) },
	}
	v := api.Snapshot{Connection: "ready", Approvals: []api.Approval{{ID: "original", Status: "pending"}}}
	o.Observe(v)
	o.Observe(api.Snapshot{Connection: "offline"})
	if len(dismissed) != 1 || dismissed[0] != "runtime-recovery" {
		t.Fatal("lost connection dismissed a pending decision", dismissed)
	}
	o.Observe(v)
	v.Approvals[0].Status = "resolved"
	o.Observe(v)
	o.Observe(v)
	if len(delivered) != 1 || len(dismissed) != 2 || dismissed[1] != "approval-original" {
		t.Fatal(delivered, dismissed)
	}
}

func TestScheduledNotificationDoesNotLeakEarlierMessagesOrToolOutput(t *testing.T) {
	var bodies []string
	o := NotificationObserver{Notify: func(_, _, body string, _ bool) { bodies = append(bodies, body) }}
	o.Observe(api.Snapshot{Connection: "ready", Phase: "idle"})
	v := api.Snapshot{Connection: "ready", Phase: "completed", Scheduled: true, CurrentTurn: "current", Items: []api.Item{
		{Kind: "assistant", TurnKey: "old", Text: "Private old reply"},
		{Kind: "tool", TurnKey: "current", Text: "Tool output"},
	}}
	o.Observe(v)
	v.Items = append(v.Items, api.Item{Kind: "assistant", TurnKey: "current", Text: api.SilentReminder})
	o.Observe(v)
	if len(bodies) != 0 {
		t.Fatal("notified content without a current visible message", bodies)
	}
	v.Items[len(v.Items)-1].Text = "**站起来活动一下。**"
	o.Observe(v)
	o.Observe(v)
	if len(bodies) != 1 || bodies[0] != "站起来活动一下。" {
		t.Fatal(bodies)
	}
}

func TestNotificationPreviewIsBoundedPlainText(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"## Break\n\n**Stand up** &amp; stretch.\n[Open notes](https://example.com/private)", "Break Stand up & stretch. Open notes"},
		{"Reminder\n\n```sh\nprivate command\n```\n\nNext step", "Reminder Next step"},
		{"\x00A\nB\tC", "A B C"},
		{"![private image](https://example.com/image)", ""},
	} {
		if got := notificationPreview(tc.input); got != tc.want {
			t.Errorf("preview = %q, want %q", got, tc.want)
		}
	}
	got := notificationPreview(strings.Repeat("提醒🌸", 100))
	if !utf8.ValidString(got) || len([]rune(got)) != 180 || !strings.HasSuffix(got, "…") {
		t.Fatal("invalid or unbounded Unicode preview")
	}
}

func TestNotificationsUseCurrentLanguageWithoutReannouncingHistory(t *testing.T) {
	locale := i18n.English
	var titles []string
	o := NotificationObserver{Locale: func() i18n.Locale { return locale }, Notify: func(_, title, _ string, _ bool) { titles = append(titles, title) }}
	v := api.Snapshot{Connection: "ready", Approvals: []api.Approval{{ID: "first", Status: "pending"}}}
	o.Observe(v)
	locale = i18n.Chinese
	o.Observe(v)
	if len(titles) != 1 {
		t.Fatal("language change reannounced a decision")
	}
	v.Approvals = append(v.Approvals, api.Approval{ID: "second", Status: "pending"})
	o.Observe(v)
	if len(titles) != 2 || titles[0] != "Your confirmation is needed" || titles[1] != "需要你的确认" {
		t.Fatal(titles)
	}
}
