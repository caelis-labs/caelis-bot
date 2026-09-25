package care

import (
	"math"
	"time"
)

// Sample contains metadata only: no window title, content, keystrokes or pixels.
// It is supplied by the native host, never a model tool or renderer.
type Sample struct {
	Presence
	IdleSeconds float64
	Application string
}
type SourcesTracker struct {
	previous  time.Time
	usage     time.Time
	minute    time.Time
	active    time.Duration
	app       string
	available bool
}

func (s *SourcesTracker) Poll(now time.Time, v Sample) []Event {
	var events []Event
	minute := now.Truncate(time.Minute)
	if minute.After(s.minute) {
		events = append(events, Event{Source: "clock.minute", At: minute, Data: map[string]any{}})
		s.minute = minute
	}
	available := v.Available() && !math.IsNaN(v.IdleSeconds) && !math.IsInf(v.IdleSeconds, 0) && v.IdleSeconds >= 0
	elapsed := now.Sub(s.previous)
	if !available || elapsed > 45*time.Second || elapsed < 0 || v.IdleSeconds >= 300 {
		s.active = 0
	}
	if available && s.available && elapsed > 0 && elapsed <= 45*time.Second && v.IdleSeconds < 120 {
		s.active += elapsed
	}
	if available && s.available && s.app != "" && v.Application != "" && v.Application != s.app {
		events = append(events, Event{Source: "desktop.appChanged", At: now, Data: map[string]any{"application": v.Application, "previousApplication": s.app}})
	}
	s.app = v.Application
	s.available = available
	s.previous = now
	if available && (s.usage.IsZero() || now.Sub(s.usage) >= 30*time.Second) {
		events = append(events, Event{Source: "desktop.usage", At: now, Data: map[string]any{"activeSeconds": int64(s.active.Seconds()), "idleSeconds": int64(v.IdleSeconds), "application": v.Application}})
		s.usage = now
	}
	return events
}
