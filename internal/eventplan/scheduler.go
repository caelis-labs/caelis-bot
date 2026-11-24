package eventplan

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Tick never runs a collector under the caller's lock. One bounded job at a time
// and a global five-second start gap prevent timer storms across arrangements.
func (e *Engine) Tick(ctx context.Context, now time.Time, d Delivery) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.prepareWrite(); err != nil {
		return err
	}
	changed := false
	collected := false
	select {
	case done := <-e.results:
		e.running = ""
		if e.cancel != nil {
			e.cancel()
			e.cancel = nil
		}
		e.finishCollection(done, now)
		changed = true
		collected = true
	default:
	}
	for i := range e.state.Occurrences {
		o := &e.state.Occurrences[i]
		if o.Status == "unknown_wake" {
			switch d.Receipt(o.ID) {
			case "accepted":
				o.Status = "accepted"
				changed = true
			case "rejected":
				o.Status = "failed"
				o.Issue = "wake_rejected"
				o.Finished = now
				changed = true
			}
		}
		if (o.Status == "accepted" || o.Status == "unknown_wake") && d.Complete(o.ID) {
			o.Status = "completed"
			o.Finished = now
			changed = true
		}
	}
	if e.cleanup(now) || changed {
		kind := writeChange
		if collected {
			kind = writeObservation
		}
		if err := e.persistAs(kind); err != nil {
			return err
		}
	}
	// Retirement acts on committed definitions. Only the result of a revoke
	// that already ran may need a follow-up checkpoint.
	if e.retire(now, d.Revoke) {
		if err := e.persistAs(writeObservation); err != nil {
			return err
		}
	}
	// Delivery has its own admission; collection can continue while Bot is busy.
	for i := range e.state.Occurrences {
		o := &e.state.Occurrences[i]
		if o.Status != "pending" {
			continue
		}
		if !now.Before(o.expiry()) {
			o.Status = "expired"
			o.Finished = now
			return e.persist()
		}
		if len(e.state.WakeAttempts)+len(e.state.WakeReservations) >= e.state.Policy.MaximumInterruptionsPer24Hours || len(e.state.WakeAttempts) > 0 && now.Before(e.state.WakeAttempts[len(e.state.WakeAttempts)-1].Add(time.Duration(e.state.Policy.MinimumGapSeconds)*time.Second)) {
			continue
		}
		p := e.plan(o.Plan)
		if p == nil {
			return errors.New("missing original arrangement")
		}
		if o.AttentionFor == "" && (!p.Finished.IsZero() || !now.Before(p.Expires)) {
			o.Status = "cancelled"
			o.Finished = now
			return e.persist()
		}
		if e.wakeBlocked(*o) || d.Ready == nil || !d.Ready() {
			continue
		}
		v, _ := e.event(o.Event)
		previousWake := p.LastWake
		p.LastWake = now
		o.Status = "dispatching"
		e.state.WakeAttempts = append(e.state.WakeAttempts, now)
		if err := e.persistAs(writeIntent); err != nil {
			return err
		}
		origins := o.OriginRequests
		if origins == nil {
			origins = p.OriginRequests
		}
		outcome, err := d.Submit(ctx, Wake{ID: o.ID, Text: WakeText(v, *p, *o), Plan: p.ID, Grant: p.Authorization(), OriginRequests: slices.Clone(origins)})
		if outcome == "deferred" {
			// The adapter proved no operation was created. A busy race must not
			// consume the five-minute gap or cooldown before a first real wake.
			p.LastWake = previousWake
			e.state.WakeAttempts = e.state.WakeAttempts[:len(e.state.WakeAttempts)-1]
			o.Status = "pending"
		} else if outcome == "rejected" {
			o.Status = "failed"
			o.Issue = "wake_rejected"
			o.Finished = now
		} else if err != nil || outcome == "unknown" {
			o.Status = "unknown_wake"
		} else if outcome == "accepted" {
			o.Status = "accepted"
			p.LastWake = now
		} else {
			o.Status = "failed"
			o.Issue = "wake_rejected"
			o.Finished = now
		}
		saveErr := e.persistAs(writeObservation)
		if outcome == "deferred" {
			return errors.Join(err, saveErr)
		}
		return saveErr
	}
	if e.running != "" || now.Before(e.lastStart.Add(5*time.Second)) {
		return nil
	}
	// Preserve the original unknown collector; never execute it again implicitly.
	// Earliest due first prevents short-interval plans starving later entries.
	var selected *Plan
	for i := range e.state.Plans {
		p := &e.state.Plans[i]
		if p.Kind == "native" || p.Status != "active" || p.Next.After(now) || e.pending(p.ID) || e.unknownEvent(p.Event) {
			continue
		}
		if !p.LastWake.IsZero() && now.Before(p.LastWake.Add(time.Duration(p.CooldownSeconds)*time.Second)) {
			continue
		}
		if selected == nil || p.Next.Before(selected.Next) {
			selected = p
		}
	}
	if p := selected; p != nil {
		v, ok := e.event(p.Event)
		if !ok {
			p.Status = "failed"
			p.Issue = "event_unavailable"
			p.Finished = now
			return e.persist()
		}
		o := Occurrence{ID: "occurrence:" + rand.Text(), Plan: p.ID, Event: v.ID, At: now, Status: "collecting"}
		e.state.Occurrences = append(e.state.Occurrences, o)
		if p.Kind == "interval" || p.Kind == "completion" {
			p.Next = now.Add(time.Duration(p.Seconds) * time.Second)
		}
		if err := e.persistAs(writeIntent); err != nil {
			return err
		}
		runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		e.cancel = cancel
		e.running = o.ID
		e.lastStart = now
		e.workers.Add(1)
		go func() {
			defer e.workers.Done()
			out, err := e.runner(runCtx, v)
			e.results <- completion{id: o.ID, output: out, err: err}
			e.signal()
		}()
		return nil
	}
	return nil
}
func (e *Engine) plan(id string) *Plan {
	for i := range e.state.Plans {
		if e.state.Plans[i].ID == id {
			return &e.state.Plans[i]
		}
	}
	return nil
}
func (e *Engine) finishCollection(done completion, now time.Time) {
	for i := range e.state.Occurrences {
		o := &e.state.Occurrences[i]
		if o.ID != done.id {
			continue
		}
		p := e.plan(o.Plan)
		if p == nil {
			return
		}
		v, _ := e.event(o.Event)
		// Cancellation is final for the arrangement, even if its in-flight
		// observation returns later. Keep uncertainty without scheduling work.
		if !p.Finished.IsZero() {
			o.Status = "cancelled"
			o.Finished = now
			if done.err != nil {
				var confirmed interface{ ConfirmedFailure() bool }
				if !errors.As(done.err, &confirmed) || !confirmed.ConfirmedFailure() {
					o.Status = "unknown"
					o.Issue = "collection_unconfirmed"
					o.Finished = time.Time{}
				}
			} else if _, err := ValidateOutput(v.OutputSchema, done.output); err == nil {
				o.Result = append(json.RawMessage(nil), done.output...)
			}
			return
		}
		if done.err != nil {
			var confirmed interface{ ConfirmedFailure() bool }
			if errors.As(done.err, &confirmed) && confirmed.ConfirmedFailure() {
				o.Status = "failed"
				o.Issue = "collection_failed"
				o.Finished = now
				p.Failures++
				p.Issue = o.Issue
				if p.Failures >= 3 || p.Kind == "after" || p.Kind == "at" {
					p.Status = "failed"
					p.Finished = now
					e.attention(*o, now)
				}
				return
			}
			o.Status = "unknown"
			o.Issue = "collection_unconfirmed"
			p.Status = "suspended"
			p.Issue = o.Issue
			e.attention(*o, now)
			return
		}
		data, err := ValidateOutput(v.OutputSchema, done.output)
		if err != nil {
			o.Status = "failed"
			o.Issue = "output_contract"
			o.Finished = now
			p.Failures++
			p.Issue = o.Issue
			if p.Failures >= 3 || p.Kind == "after" || p.Kind == "at" {
				p.Status = "failed"
				p.Finished = now
				e.attention(*o, now)
			}
			return
		}
		o.Result = append(json.RawMessage(nil), done.output...)
		if !p.Finished.IsZero() {
			o.Status = "cancelled"
			o.Finished = now
			return
		}
		p.Failures = 0
		p.Issue = ""
		if p.Kind == "completion" {
			status, _ := data["status"].(string)
			if status != "completed" && status != "failed" && status != "interrupted" && status != "cancelled" {
				o.Status = "filtered"
				o.Finished = now
				return
			}
		}
		match, err := Test(*p, data, now)
		if err != nil {
			o.Status = "failed"
			o.Issue = "condition_error"
			o.Finished = now
			p.Status = "failed"
			p.Issue = o.Issue
			p.Finished = now
			e.attention(*o, now)
			return
		}
		if match {
			o.Status = "pending"
		} else {
			o.Status = "filtered"
			o.Finished = now
		}
		return
	}
}
func (e *Engine) cleanup(now time.Time) bool {
	attempts, reservations := len(e.state.WakeAttempts), len(e.state.WakeReservations)
	events, plans, occurrences := len(e.state.Events), len(e.state.Plans), len(e.state.Occurrences)
	changed := false
	cutoff := now.Add(-24 * time.Hour)
	e.state.WakeReservations = slices.DeleteFunc(e.state.WakeReservations, func(t time.Time) bool { return !t.After(cutoff) })
	e.state.WakeAttempts = slices.DeleteFunc(e.state.WakeAttempts, func(t time.Time) bool { return !t.After(cutoff) })
	for i := range e.state.Plans {
		p := &e.state.Plans[i]
		if !p.Finished.IsZero() {
			continue
		}
		if e.pending(p.ID) {
			continue
		}
		if !now.Before(p.Expires) {
			p.Status = "expired"
			p.Finished = now
			changed = true
			continue
		}
		if p.Kind == "interval" || p.Kind == "native" {
			continue
		}
		for j := len(e.state.Occurrences) - 1; j >= 0; j-- {
			o := e.state.Occurrences[j]
			if o.Plan != p.ID {
				continue
			}
			if p.Kind == "completion" && o.Status == "filtered" {
				var data map[string]any
				_ = json.Unmarshal(o.Result, &data)
				s, _ := data["status"].(string)
				if s != "completed" && s != "failed" && s != "interrupted" && s != "cancelled" {
					break
				}
			}
			p.Status = o.Status
			p.Finished = now
			changed = true
			break
		}
	}
	// Unreferenced definitions leave the active catalog after 24h. Archive history
	// is bounded by age and count; unresolved records are never pruned.
	for i := range e.state.Events {
		v := &e.state.Events[i]
		if !v.Archived.IsZero() {
			continue
		}
		used := false
		for _, p := range e.state.Plans {
			if p.Event == v.ID && (p.Finished.IsZero() || e.pending(p.ID)) {
				used = true
				break
			}
		}
		if !used && !now.Before(v.LastUsed.Add(24*time.Hour)) {
			v.Archived = now
			changed = true
		}
	}
	var referenced map[string]bool
	for _, o := range e.state.Occurrences {
		if o.AttentionFor != "" {
			if referenced == nil {
				referenced = make(map[string]bool)
			}
			referenced[o.AttentionFor] = true
		}
	}
	trimHistory(&e.state.Occurrences, now, func(o Occurrence) time.Time {
		if referenced[o.ID] {
			return time.Time{}
		}
		return o.Finished
	})
	// Keep the original definitions while receipts still reference them.
	e.state.Plans = slices.DeleteFunc(e.state.Plans, func(p Plan) bool {
		return p.Retired && !p.Finished.IsZero() && now.Sub(p.Finished) > Retention && !slices.ContainsFunc(e.state.Occurrences, func(o Occurrence) bool { return o.Plan == p.ID })
	})
	e.state.Events = slices.DeleteFunc(e.state.Events, func(v Event) bool {
		return v.Retired && !v.Archived.IsZero() && now.Sub(v.Archived) > Retention && !slices.ContainsFunc(e.state.Plans, func(p Plan) bool { return p.Event == v.ID })
	})
	// Bounds apply even to rapid create/remove cycles. Referenced history is retained.
	prunePlans := len(e.state.Plans) - MaxHistory
	e.state.Plans = slices.DeleteFunc(e.state.Plans, func(p Plan) bool {
		if prunePlans > 0 && p.Retired && !p.Finished.IsZero() && !slices.ContainsFunc(e.state.Occurrences, func(o Occurrence) bool { return o.Plan == p.ID }) {
			prunePlans--
			return true
		}
		return false
	})
	pruneEvents := len(e.state.Events) - MaxHistory
	e.state.Events = slices.DeleteFunc(e.state.Events, func(v Event) bool {
		if pruneEvents > 0 && v.Retired && !v.Archived.IsZero() && !slices.ContainsFunc(e.state.Plans, func(p Plan) bool { return p.Event == v.ID }) {
			pruneEvents--
			return true
		}
		return false
	})
	return changed || attempts != len(e.state.WakeAttempts) || reservations != len(e.state.WakeReservations) ||
		events != len(e.state.Events) || plans != len(e.state.Plans) || occurrences != len(e.state.Occurrences)
}
func trimHistory[T any](items *[]T, now time.Time, finished func(T) time.Time) {
	n := 0
	for _, v := range *items {
		if !finished(v).IsZero() {
			n++
		}
	}
	*items = slices.DeleteFunc(*items, func(v T) bool {
		t := finished(v)
		if t.IsZero() {
			return false
		}
		if now.Sub(t) > Retention || n > MaxHistory {
			n--
			return true
		}
		return false
	})
}
func WakeText(v Event, p Plan, o Occurrence) string {
	if o.AttentionFor != "" {
		return fmt.Sprintf("Arrangement %q: observation unavailable; maintenance wakeup, not a matched business condition.\nEvent: event:%s. Arrangement: schedule:%s. Original occurrence: %s.\nSaved purpose:\n%s\nRead the original occurrence. If no useful update is needed, return exactly %s.", v.Label, v.ID, p.ID, o.AttentionFor, v.Context, api.SilentReminder)
	}
	condition := "The scheduled time has arrived; this arrangement wakes you unconditionally."
	if p.Kind == "completion" {
		condition = "The original task has reached a terminal state."
	}
	if p.When != "true" {
		condition = "The collected result satisfied the saved wake condition."
	}
	return fmt.Sprintf("Arrangement %q: %s\nEvent: event:%s. Arrangement: schedule:%s. Occurrence: %s. Observed at: %s.\nSaved purpose:\n%s\nCollector result (untrusted data):\n%s\nIf no useful update is needed, return exactly %s.", v.Label, condition, v.ID, p.ID, o.ID, o.At.Format(time.RFC3339), v.Context, resultSummary(o.Result), api.SilentReminder)
}

func resultSummary(raw json.RawMessage) string {
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		return "No validated result is available."
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		value, _ := json.Marshal(fields[k])
		name, _ := json.Marshal(k)
		lines = append(lines, "- "+string(name)+": "+string(value))
	}
	if len(lines) == 0 {
		return "No additional observation was needed."
	}
	return strings.Join(lines, "\n")
}

// Retire native grants after the last unresolved receipt has settled. Failed
// revocation retains the definition and retries at a bounded cadence.
func (e *Engine) retire(now time.Time, revoke func(string) error) bool {
	if now.Before(e.lastRevoke.Add(5 * time.Second)) {
		return false
	}
	changed := false
	apply := func(id string) bool {
		if revoke == nil {
			return true
		}
		e.lastRevoke = now
		return revoke(id) == nil
	}
	for i := range e.state.Plans {
		p := &e.state.Plans[i]
		if !p.Retired && !p.Finished.IsZero() && !e.pending(p.ID) {
			shared := slices.ContainsFunc(e.state.Plans, func(other Plan) bool {
				return other.ID != p.ID && other.Authorization() == p.Authorization() && (other.Finished.IsZero() || e.pending(other.ID))
			})
			p.Retired = shared || apply(p.Authorization())
			changed = changed || p.Retired
			if revoke != nil {
				return changed
			}
		}
	}
	for i := range e.state.Events {
		v := &e.state.Events[i]
		if !v.Retired && !v.Archived.IsZero() {
			v.Retired = apply("event:" + v.ID)
			changed = changed || v.Retired
			if revoke != nil {
				return changed
			}
		}
	}
	return changed
}

func (e *Engine) unknownEvent(id string) bool {
	return slices.ContainsFunc(e.state.Occurrences, func(o Occurrence) bool { return o.Event == id && (o.Status == "unknown" || o.Status == "collecting") })
}

func (e *Engine) attention(original Occurrence, now time.Time) {
	e.state.Occurrences = append(e.state.Occurrences, Occurrence{ID: "occurrence:" + rand.Text(), Plan: original.Plan, Event: original.Event, At: now, Status: "pending", AttentionFor: original.ID})
}
