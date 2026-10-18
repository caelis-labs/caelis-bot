package desktop

import "testing"

func TestNotificationAttentionPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		chat, pet, reminder, deliver bool
	}{
		{"decision already visible on pet", false, true, false, false},
		{"hidden pet still allows decisions", false, false, false, true},
		{"due reminder with visible pet", false, true, true, true},
		{"due reminder with hidden pet", false, false, true, true},
		{"active conversation suppresses decision", true, false, false, false},
		{"active conversation suppresses reminder", true, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldDeliverNotification(tc.chat, tc.pet, tc.reminder); got != tc.deliver {
				t.Fatalf("delivery = %v, want %v", got, tc.deliver)
			}
		})
	}
}
