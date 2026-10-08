package bot

import (
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const (
	dreamOpportunity       = 15 * time.Minute
	dreamReserve           = 3 * time.Minute // two-minute execution budget plus margin
	dreamCooldown          = 30 * time.Minute
	dreamGrowth      int64 = 8000
	dreamIdle              = 2 * time.Minute
)

// DreamEnvironment contains native metadata only. Unknown presence fails closed.
// Epoch changes on native sleep/wake and lock/unlock, including brief transitions
// between polls. DraftRevision only postpones admission while the user is editing.
type DreamEnvironment struct {
	Available     bool
	Reason        string // Host-generated presence category; never application text.
	Epoch         uint64
	DraftRevision uint64
}

type dreamPolicy struct {
	sample                                                  func() DreamEnvironment
	previous, notBefore, stableAfter, idleSince, draftUntil time.Time
	epoch, draftRevision                                    uint64
	available                                               bool
	environmentAvailable                                    bool
	environmentReason                                       string
	session                                                 string
	window, lastUsed                                        int64
	baseline                                                int64
	reason                                                  string
}

func (r *Runtime) ConfigureDreamEnvironment(sample func() DreamEnvironment) {
	r.step.Lock()
	defer r.step.Unlock()
	if r.dream != nil {
		r.dream.policy.sample = sample
	}
}

// Wall time intentionally includes system sleep (Go's monotonic clock may not).
// A discontinuity consumes this opportunity; it never queues a catch-up request.
func (p *dreamPolicy) observe(now time.Time, s api.ConversationState) {
	env := DreamEnvironment{}
	if p.sample != nil {
		env = p.sample()
	}
	available := env.Available && s.Observed
	gap := now.Sub(p.previous)
	broken := !available || (!p.previous.IsZero() && (gap < 0 || gap > 45*time.Second || env.Epoch != p.epoch))
	if broken {
		// Preserve the invalidated observation even across a backward clock jump;
		// waiting for wall time to catch up must not make that same event fresh.
		if now.After(p.notBefore) {
			p.notBefore = now
		}
		if s.Usage.ModelAt.After(p.notBefore) {
			p.notBefore = s.Usage.ModelAt
		}
		p.stableAfter, p.idleSince = now.Add(time.Minute), time.Time{}
	} else if !p.available {
		p.stableAfter = now.Add(time.Minute)
	}
	if env.DraftRevision != p.draftRevision {
		p.draftUntil = now.Add(time.Minute)
		p.idleSince = time.Time{}
	}
	if !available || !s.Idle || now.Before(p.draftUntil) {
		p.idleSince = time.Time{}
	} else if p.idleSince.IsZero() {
		p.idleSince = now
	}
	p.previous, p.epoch, p.draftRevision, p.available, p.environmentAvailable = now, env.Epoch, env.DraftRevision, available, env.Available
	p.environmentReason = env.Reason
	if p.environmentReason == "" {
		p.environmentReason = "unknown"
	}
}

func (p *dreamPolicy) idleReason(now time.Time, s api.ConversationState, at time.Time) string {
	if !p.available || now.Before(p.stableAfter) {
		return "environment_unstable"
	}
	if !s.Idle || s.Turn == "" || s.Status != "completed" || p.idleSince.IsZero() || now.Before(p.draftUntil) {
		return "busy"
	}
	if now.Sub(p.idleSince) < dreamIdle || now.Sub(at) < dreamIdle {
		return "idle_short"
	}
	return "ready"
}

func (p *dreamPolicy) decide(now time.Time, s api.ConversationState, state dreamState) string {
	u := s.Usage
	if u.Window > 0 && u.Used >= 0 && !u.ModelAt.IsZero() && u.ModelAt.After(p.notBefore) && !u.ModelAt.After(now) {
		if p.session != s.Session {
			p.session, p.window, p.baseline = s.Session, u.Window, 0
			if state.BaselineSession == s.Session {
				// Compaction may have lowered usage since the last persisted
				// attempt. Rebase from fresh native evidence after a restart too.
				p.baseline = min(state.BaselineUsed, u.Used)
			}
		} else if p.window != u.Window || u.Used < p.lastUsed {
			// A changed model window or native compaction establishes a new baseline.
			p.baseline = u.Used
		}
		p.window, p.lastUsed = u.Window, u.Used
	}
	if reason := p.idleReason(now, s, state.At); reason != "ready" {
		return reason
	}
	if !state.Dirty {
		return "no_new_activity"
	}
	if u.Window <= 0 || u.Used < 0 || u.ModelAt.IsZero() {
		return "usage_unknown"
	}
	if !u.ModelAt.After(p.notBefore) || u.ModelAt.After(now) {
		return "fresh_activity_required"
	}
	if now.Sub(u.ModelAt) >= dreamOpportunity-dreamReserve {
		return "opportunity_expired"
	}
	if float64(u.Used)/float64(u.Window) < .5 {
		return "context_small"
	}
	if u.Used-p.baseline < dreamGrowth {
		return "growth_small"
	}
	if !state.LastAttempt.IsZero() && now.Sub(state.LastAttempt) < dreamCooldown {
		return "cooldown"
	}
	return "ready"
}
