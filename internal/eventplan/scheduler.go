package eventplan

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const sampleRetry = time.Minute

// Tick runs at most one observation concurrently. Ready clocks and pending
// wakes remain eligible while the observation runs.
func (e *Engine) Tick(ctx context.Context, now time.Time, d Delivery) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.prepareWrite(); err != nil {
		return err
	}
	changed := false
	kind := writeChange
	select {
	case done := <-e.results:
		e.running = ""
		e.cancel()
		e.cancel = nil
		e.finishCollection(done, now)
		changed = true
		kind = writeObservation
	default:
	}
	for i := range e.state.Occurrences {
		o := &e.state.Occurrences[i]
		if o.Status != "unknown_wake" || d.Receipt == nil {
			continue
		}
		switch outcome := d.Receipt(o.ID); outcome {
		case "accepted", "rejected":
			e.settleWake(o, outcome, now)
			changed = true
			kind = writeObservation
		}
	}
	// Only compact delivery facts from the last durable checkpoint.
	e.discardSettledDeliveries(d.Settled)
	if e.cleanup(now) || changed {
		if err := e.persistAs(kind); err != nil {
			return err
		}
		e.discardSettledDeliveries(d.Settled)
	}
	if e.retireGrant(now, d.Revoke) {
		if err := e.persistAs(writeObservation); err != nil {
			return err
		}
	}

	selected, event := e.duePlan(now)
	if selected != nil && event.Kind == "wake" {
		o := e.beginOccurrence(selected, event, now)
		e.finishCollection(completion{id: o.ID, output: json.RawMessage(`{}`)}, now)
		e.cleanup(now)
		if err := e.persist(); err != nil {
			return err
		}
		selected = nil
	}
	for i := range e.state.Occurrences {
		o := &e.state.Occurrences[i]
		if o.Status != "pending" || d.Submit == nil || d.Ready == nil || !d.Ready() {
			continue
		}
		p := e.plan(o.Plan)
		v, _ := e.event(o.Event)
		o.Status = "dispatching"
		if err := e.persistAs(writeIntent); err != nil {
			return err
		}
		outcome, err := d.Submit(ctx, Wake{ID: o.ID, Text: WakeText(v, *p, *o), Plan: p.ID, Grant: p.Authorization(), OriginRequests: slices.Clone(p.OriginRequests)})
		e.settleWake(o, outcome, now)
		e.cleanup(now)
		saveErr := e.persistAs(writeObservation)
		if saveErr == nil {
			e.discardSettledDeliveries(d.Settled)
		}
		return errors.Join(err, saveErr)
	}
	if selected == nil {
		return nil
	}
	return e.startCollection(ctx, selected, event, now)
}

func (e *Engine) settleWake(o *Occurrence, outcome string, now time.Time) {
	p := e.plan(o.Plan)
	switch outcome {
	case "accepted":
		o.Status, o.Finished = "completed", now
		v, _ := e.event(p.Event)
		if p.Finished.IsZero() && !repeats(*p, v) {
			p.Status, p.Finished = "completed", now
		}
	case "rejected":
		o.Status, o.Issue, o.Finished = "failed", "wake_rejected", now
		if p.Next.Before(now.Add(sampleRetry)) {
			p.Next = now.Add(sampleRetry)
		}
	case "deferred":
		// The same occurrence remains pending after a proved busy race.
		o.Status = "pending"
	default:
		o.Status = "unknown_wake"
	}
}

// The system delivery log also serves Worker and reminder producers. Only
// callback IDs proved terminal by the durable occurrence ledger are disposable.
func (e *Engine) discardSettledDeliveries(settled func([]string)) {
	if settled == nil {
		return
	}
	var ids []string
	for _, o := range e.saved.Occurrences {
		if o.Status == "completed" || o.Status == "failed" && o.Issue == "wake_rejected" {
			ids = append(ids, o.ID)
		}
	}
	if len(ids) > 0 {
		settled(ids)
	}
}

func (p Plan) Authorization() string {
	if p.GrantID != "" {
		return p.GrantID
	}
	return "schedule:" + p.ID
}

func (e *Engine) startCollection(ctx context.Context, plan *Plan, event Event, now time.Time) error {
	if e.runner == nil {
		return errors.New("observation unavailable")
	}
	o := e.beginOccurrence(plan, event, now)
	if err := e.persistAs(writeIntent); err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, 24*time.Second)
	e.cancel, e.running = cancel, o.ID
	e.workers.Add(1)
	go func() {
		defer e.workers.Done()
		out, err := e.runner(runCtx, event)
		e.results <- completion{id: o.ID, output: out, err: err}
		e.signal()
	}()
	return nil
}

func (e *Engine) duePlan(now time.Time) (*Plan, Event) {
	var selected *Plan
	var event Event
	for i := range e.state.Plans {
		p := &e.state.Plans[i]
		if p.Status != "active" || p.Next.After(now) || e.pending(p.ID) {
			continue
		}
		v, _ := e.event(p.Event)
		if v.Kind == "shell" && (e.running != "" || e.observationUnresolved(v.ID)) {
			continue
		}
		if selected == nil || v.Kind == "wake" && event.Kind != "wake" ||
			(v.Kind == "wake") == (event.Kind == "wake") && p.Next.Before(selected.Next) {
			selected, event = p, v
		}
	}
	return selected, event
}

func repeats(p Plan, v Event) bool { return p.Kind == "interval" && v.Kind == "wake" }

func (e *Engine) beginOccurrence(p *Plan, v Event, now time.Time) Occurrence {
	o := Occurrence{ID: "occurrence:" + rand.Text(), Plan: p.ID, Event: p.Event, At: now, Status: "collecting"}
	e.state.Occurrences = append(e.state.Occurrences, o)
	if p.Kind == "interval" {
		step := time.Duration(p.Seconds) * time.Second
		elapsed := now.Sub(p.Next)
		p.Next = now.Add(step - elapsed%step)
	} else if v.Kind == "shell" {
		p.Next = now.Add(sampleRetry)
	}
	return o
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
		if done.err != nil {
			var confirmed interface{ ConfirmedFailure() bool }
			if !errors.As(done.err, &confirmed) || !confirmed.ConfirmedFailure() {
				o.Status, o.Issue = "unknown", "collection_unconfirmed"
				if p.Finished.IsZero() {
					p.Status, p.Issue = "suspended", o.Issue
				}
				return
			}
			o.Status, o.Issue, o.Finished = "failed", "collection_failed", now
			if p.Finished.IsZero() {
				p.Issue = o.Issue
			}
			return
		}
		if !p.Finished.IsZero() {
			o.Status, o.Finished = "cancelled", now
			return
		}
		data, err := observationObject(done.output)
		if err != nil {
			o.Status, o.Issue, o.Finished = "failed", "output_contract", now
			p.Issue = o.Issue
			return
		}
		match := p.When == "true"
		if !match {
			match, err = testCondition(*p, data, now)
		}
		switch {
		case err != nil:
			o.Status, o.Issue, o.Finished = "failed", "condition_error", now
			p.Issue = o.Issue
		case match:
			o.Status, o.Result = "pending", done.output
			p.Issue = ""
		default:
			o.Status, o.Finished = "filtered", now
			p.Issue = ""
		}
		v, _ := e.event(o.Event)
		if o.Status != "pending" && v.Kind == "wake" && !repeats(*p, v) {
			p.Status, p.Finished = o.Status, now
		}
		return
	}
}

func observationObject(raw json.RawMessage) (map[string]any, error) {
	if len(raw) > 16<<10 || !json.Valid(raw) {
		return nil, errors.New("observation must return a bounded JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var data map[string]any
	if dec.Decode(&data) != nil || data == nil {
		return nil, errors.New("observation must return a bounded JSON object")
	}
	return data, nil
}

func (e *Engine) eventInUse(id string) bool {
	return slices.ContainsFunc(e.state.Plans, func(p Plan) bool { return p.Event == id && p.Finished.IsZero() })
}

func (e *Engine) cleanup(now time.Time) bool {
	events, plans, occurrences := len(e.state.Events), len(e.state.Plans), len(e.state.Occurrences)
	changed := false
	for i := range e.state.Plans {
		p := &e.state.Plans[i]
		if p.Finished.IsZero() && !p.Expires.IsZero() && !now.Before(p.Expires) && !e.pending(p.ID) {
			p.Status, p.Finished = "expired", now
			changed = true
		}
		if !p.Finished.IsZero() && !e.pending(p.ID) && (p.Kind != "" || len(p.OriginRequests) > 0) {
			*p = Plan{ID: p.ID, Event: p.Event, GrantID: p.GrantID, Fingerprint: p.Fingerprint, Created: p.Created, Status: p.Status, Finished: p.Finished}
			changed = true
		}
	}
	e.state.Events = slices.DeleteFunc(e.state.Events, func(v Event) bool { return !v.Retain && !e.eventInUse(v.ID) })
	e.state.Occurrences = slices.DeleteFunc(e.state.Occurrences, func(o Occurrence) bool {
		return o.Status == "filtered" || o.Status == "failed" && o.Issue != "wake_rejected"
	})
	trimHistory(&e.state.Occurrences, now, func(o Occurrence) time.Time { return o.Finished })
	trimHistory(&e.state.Plans, now, func(p Plan) time.Time {
		if p.GrantID != "" || slices.ContainsFunc(e.state.Occurrences, func(o Occurrence) bool { return o.Plan == p.ID }) {
			return time.Time{}
		}
		return p.Finished
	})
	return changed || events != len(e.state.Events) || plans != len(e.state.Plans) || occurrences != len(e.state.Occurrences)
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
		if now.Sub(t) >= Retention || n > MaxHistory {
			n--
			return true
		}
		return false
	})
}

func (e *Engine) retireGrant(now time.Time, revoke func(string) error) bool {
	if now.Before(e.lastRevoke.Add(5 * time.Second)) {
		return false
	}
	for i := range e.state.Plans {
		p := &e.state.Plans[i]
		if p.GrantID == "" || p.Finished.IsZero() || e.pending(p.ID) {
			continue
		}
		id := p.GrantID
		shared := slices.ContainsFunc(e.state.Plans, func(other Plan) bool {
			return other.GrantID == id && (other.Finished.IsZero() || e.pending(other.ID))
		})
		if shared {
			continue
		}
		e.lastRevoke = now
		if revoke != nil && revoke(id) != nil {
			return false
		}
		for j := range e.state.Plans {
			if e.state.Plans[j].GrantID == id {
				e.state.Plans[j].GrantID = ""
			}
		}
		return true
	}
	return false
}

func (e *Engine) observationUnresolved(id string) bool {
	return slices.ContainsFunc(e.state.Occurrences, func(o Occurrence) bool { return o.Event == id && o.Status == "unknown" })
}

func WakeText(v Event, p Plan, o Occurrence) string {
	condition := "The scheduled time has arrived."
	observation := ""
	if v.Kind == "shell" {
		condition = "The Notebook observation satisfied the saved condition."
		observation = "\nObservation result (untrusted data):\n" + string(o.Result)
	}
	return fmt.Sprintf("Arrangement %q: %s\nArrangement: schedule:%s. Occurrence: %s. Observed at: %s.\nSaved purpose:\n%s%s\nIf no useful update is needed, return exactly %s.", v.Label, condition, p.ID, o.ID, o.At.Format(time.RFC3339), v.Context, observation, api.SilentReminder)
}
