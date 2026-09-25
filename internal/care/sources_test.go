package care

import (
	"testing"
	"time"
)

func usage(events []Event) int64 {
	for _, e := range events {
		if e.Source == "desktop.usage" {
			return e.Data["activeSeconds"].(int64)
		}
	}
	return -1
}
func TestNativeUsageResetsWithoutCountingSleepOrAbsence(t *testing.T) {
	var s SourcesTracker
	v := Sample{Presence: presence(), Application: "editor", IdleSeconds: 0}
	if n := usage(s.Poll(instant, v)); n != 0 {
		t.Fatal(n)
	}
	if n := usage(s.Poll(instant.Add(30*time.Second), v)); n != 30 {
		t.Fatal(n)
	}
	v.Application = "terminal"
	ev := s.Poll(instant.Add(31*time.Second), v)
	if len(ev) != 1 || ev[0].Source != "desktop.appChanged" || ev[0].Data["previousApplication"] != "editor" {
		t.Fatal(ev)
	}
	if n := usage(s.Poll(instant.Add(3*time.Hour), v)); n != 0 {
		t.Fatal("counted sleep", n)
	}
	v.IdleSeconds = 301
	if n := usage(s.Poll(instant.Add(3*time.Hour+30*time.Second), v)); n != 0 {
		t.Fatal("did not reset break", n)
	}
	v.Presence = Presence{Awake: true}
	v.IdleSeconds = 0
	s.Poll(instant.Add(3*time.Hour+60*time.Second), v)
	v.Presence = presence()
	if n := usage(s.Poll(instant.Add(3*time.Hour+90*time.Second), v)); n != 0 {
		t.Fatal("counted unknown presence", n)
	}
}
func TestClockCoalescesAndUsesRuleTimezone(t *testing.T) {
	var s SourcesTracker
	ev := s.Poll(instant, Sample{})
	if len(ev) != 1 || ev[0].Source != "clock.minute" {
		t.Fatal(ev)
	}
	if len(s.Poll(instant.Add(time.Second), Sample{})) != 0 {
		t.Fatal("duplicate minute")
	}
	if len(s.Poll(instant.Add(48*time.Hour), Sample{})) != 1 {
		t.Fatal("replayed missed minutes")
	}
}
