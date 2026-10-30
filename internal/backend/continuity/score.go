package continuity

import "time"

const Cooldown = 7 * 24 * time.Hour

// These are conservative invitation pacing/pattern choices, not calibrated
// health limits. There is no token-scale score or inferred stale-content value.
const (
	minimumObservedSpan     = 14 * 24 * time.Hour
	minimumWorkTurns        = 64
	maintenanceLookback     = 8
	minimumMaintenanceTurns = 3
)

type Evidence struct {
	WorkSpanKnown     bool
	WorkSpan          bool
	RecentMaintenance int
	LatestMaintenance bool
}

func (e Evidence) Invites() bool {
	return e.WorkSpan && e.LatestMaintenance && e.RecentMaintenance >= minimumMaintenanceTurns
}

func Evaluate(o Observation, sinceTurns uint64, notBefore, now time.Time) Evidence {
	e := Evidence{WorkSpanKnown: !o.Since.IsZero() && !o.Since.After(now)}
	e.WorkSpan = e.WorkSpanKnown && now.Sub(o.Since) >= minimumObservedSpan && o.Turns >= sinceTurns && o.Turns-sinceTurns >= minimumWorkTurns
	var previous uint64
	for _, m := range o.Maintenance {
		if m.Turn == 0 {
			continue
		}
		if m.Turn <= previous || m.Turn > o.Turns || m.At.IsZero() || m.At.After(now) {
			// Corrupt or incompatible persisted observations cannot invite a review.
			e.RecentMaintenance, e.LatestMaintenance = 0, false
			return e
		}
		previous = m.Turn
		if m.Turn <= sinceTurns || o.Turns-m.Turn >= maintenanceLookback || m.At.Before(notBefore) {
			continue
		}
		e.RecentMaintenance++
		e.LatestMaintenance = m.Turn == o.Turns
	}
	return e
}
