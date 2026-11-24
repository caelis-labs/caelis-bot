package bot

import (
	"context"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// RequestTick signals changed admission or work without waiting for delivery.
func (r *Runtime) RequestTick() {
	select {
	case r.wakeup <- struct{}{}:
	default:
	}
}

func (r *Runtime) run(ctx context.Context) {
	defer close(r.done)
	var events <-chan struct{}
	if r.events != nil {
		events = r.events.Changes()
	}
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var alarm <-chan time.Time
	for {
		if next := r.nextTick(r.now()); !next.IsZero() {
			timer.Reset(next.Sub(r.now()))
			alarm = timer.C
		}
		select {
		case <-ctx.Done():
			return
		case <-alarm:
		case <-r.wakeup:
		case <-events:
		}
		timer.Stop()
		alarm = nil
		if ctx.Err() != nil {
			return
		}
		_ = r.Tick(ctx)
	}
}

func (r *Runtime) reportWork(reports api.TaskReporter) (pending, poll bool) {
	if r.deliveryErr != nil {
		return false, false
	}
	if source, ok := reports.(interface{ ReportWork() (bool, bool) }); ok {
		return source.ReportWork()
	}
	return reports != nil, reports != nil
}

// There is no clock while all producers are idle. Bounded retries are kept
// for due input and active Workers on independently observed owners.
func (r *Runtime) nextTick(now time.Time) time.Time {
	r.step.Lock()
	defer r.step.Unlock()
	if r.paused || r.engine == nil {
		return time.Time{}
	}
	scheduled := true
	if source, ok := r.engine.(api.ApplicationCapabilityProvider); ok {
		scheduled = source.ApplicationCapabilities().ScheduledActivation
	}
	introduction := ""
	if r.initialization != nil {
		introduction = r.initialization.Initialization().Status
		scheduled = scheduled && introduction == "accepted"
	}
	r.mu.Lock()
	reports := r.reports
	wake := r.state.Wake
	var next time.Time
	offer := func(at time.Time) {
		if !at.IsZero() && (next.IsZero() || at.Before(next)) {
			next = at
		}
	}
	retry := now.Add(time.Second)
	legacyBlocked := wake != nil && wake.Status != "accepted"
	if scheduled && !legacyBlocked {
		for _, s := range r.state.Schedules {
			if s.Runtime == r.provider && s.Enabled {
				offer(s.Next)
			}
		}
	} else if scheduled && wake.Runtime == r.provider && wake.Status == "pending" {
		offer(retry)
	}
	r.mu.Unlock()
	if _, poll := r.reportWork(reports); poll {
		offer(retry)
	}
	if introduction == "pending" {
		offer(retry)
	}
	if r.review != nil && r.review.err == nil && r.review.state.Pending != nil {
		offer(retry)
	}
	if r.idle == nil && r.handoff != nil {
		pending := r.handoff.pending()
		if pending.CallID != "" && pending.Turn != "" && pending.NewSession == "" {
			offer(retry)
		}
	}
	if scheduled && r.events != nil && !(legacyBlocked && wake.Runtime == r.provider && (wake.Status == "unknown" || wake.Status == "dispatching")) {
		offer(r.events.NextCheck(now))
		if r.nativeErr == nil && r.events.HasNativePlans() {
			offer(retry)
		}
	}
	// A still-due producer can be deferred by admission or storage. It can
	// retry on an external edge or the next bounded attempt without spinning.
	if !next.IsZero() && !next.After(now) {
		return retry
	}
	return next
}
