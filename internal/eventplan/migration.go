package eventplan

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"
)

// Migration records the read-only transfer of original care delivery IDs.
type Migration struct {
	Version   int       `json:"version"`
	Digest    string    `json:"digest"`
	Completed time.Time `json:"completed"`
}

// Upgrade the prior unified ledger. Only clock and Notebook definitions can
// run. Wake intents already submitted retain their IDs, even if their source
// is now retired.
func (e *Engine) upgrade(raw []byte, now time.Time) {
	var legacy struct {
		Events []struct {
			ID       string
			Archived time.Time
			Retired  bool
		}
		Plans []struct {
			ID, Kind string
			Retired  bool
		}
		Occurrences []struct {
			ID, AttentionFor string
		}
	}
	_ = json.Unmarshal(raw, &legacy)
	archived := map[string]bool{}
	retiredEvents := map[string]bool{}
	for _, v := range legacy.Events {
		archived[v.ID] = !v.Archived.IsZero()
		retiredEvents[v.ID] = v.Retired
	}
	for _, p := range legacy.Plans {
		if current := e.plan(p.ID); current != nil {
			if p.Retired {
				current.GrantID = ""
			} else if current.GrantID == "" {
				// Older runtimes keyed schedule grants by this handle rather
				// than storing an explicit grant ID in the plan.
				current.GrantID = current.Authorization()
			}
		}
	}
	for i := range e.state.Plans {
		p := &e.state.Plans[i]
		v, exists := e.event(p.Event)
		executable := exists && !archived[p.Event] && validateEvent(v) == nil && validTiming(*p)
		if !executable && p.Finished.IsZero() {
			p.Status, p.Finished = "cancelled", now

		}
		if executable && p.Until == "" && (repeats(*p, v) || v.Kind == "shell") {
			p.Expires = time.Time{}
		}
		if executable && p.Fingerprint == "" {
			v.Created = time.Time{}
			p.Fingerprint = fingerprint(*p, &v)
		}
	}
	maintenance := map[string]bool{}
	for _, o := range legacy.Occurrences {
		maintenance[o.ID] = o.AttentionFor != ""
	}
	for i := range e.state.Occurrences {
		o := &e.state.Occurrences[i]
		p := e.plan(o.Plan)
		if p == nil {
			continue // Validation reports a broken reference.
		}
		switch o.Status {
		case "accepted", "completed":
			finished := o.Finished
			if finished.IsZero() {
				finished = now
			}
			e.settleWake(o, "accepted", finished)
		case "dispatching", "unknown_wake", "unknown":
			// Keep unresolved original receipts for this Runtime.
		case "pending":
			if !p.Finished.IsZero() || maintenance[o.ID] {
				o.Status, o.Finished = "cancelled", now
			}
		case "collecting":
			if !p.Finished.IsZero() {
				o.Status, o.Issue = "unknown", "collection_unconfirmed"
			}
		}
	}
	// Older registrations held separate event-source grants. Their collectors
	// are retired. Wake submission used the plan grant, reconciled above.
	for _, v := range e.state.Events {
		if slices.Contains([]string{"tool", "script", "task"}, v.Kind) && !retiredEvents[v.ID] && identifier.MatchString(v.ID) {
			id := legacyPlanID("grant:" + v.ID)
			e.state.Plans = append(e.state.Plans, Plan{ID: id, Event: v.ID, GrantID: "event:" + v.ID, Status: "cancelled", Created: now, Finished: now})
		}
	}
	e.state.Events = slices.DeleteFunc(e.state.Events, func(v Event) bool { return archived[v.ID] || validateEvent(v) != nil })
	e.state.Version = 2
	e.cleanup(now)
}

type legacyState struct {
	Version int
	Rules   []struct {
		ID string
	}
	Activations []struct {
		ID, RuleID, Status, Result string
		DispatchedAt               time.Time
	}
}

func (e *Engine) MigrateLegacy(path string, now time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.prepareWrite(); err != nil {
		return errors.Join(ErrWritePending, err)
	}
	if e.state.Migration != nil {
		return nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		e.state.Migration = &Migration{Version: 1, Digest: "absent", Completed: now}
		return e.persistAs(writeObservation)
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return errors.New("saved native arrangements unavailable; original data preserved")
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.New("saved native arrangements unavailable; original data preserved")
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	closeErr := file.Close()
	var legacy legacyState
	if readErr != nil || closeErr != nil || len(raw) > 2<<20 || json.Unmarshal(raw, &legacy) != nil ||
		(legacy.Version != 1 && legacy.Version != 2) || len(legacy.Rules) > 32 || len(legacy.Activations) > 256 {
		return errors.New("invalid saved native arrangements; original data preserved")
	}
	next := copyState(e.state)
	plans := map[string]bool{}
	seen := map[string]bool{}
	for _, p := range next.Plans {
		plans[p.ID] = true
	}
	for _, o := range next.Occurrences {
		seen[o.ID] = true
	}
	for _, rule := range legacy.Rules {
		if !identifier.MatchString(rule.ID) || !appendRetiredRule(&next, plans, rule.ID, now) {
			return errors.New("invalid saved native rule; original data preserved")
		}
	}
	for _, a := range legacy.Activations {
		if a.Status != "dispatching" && a.Status != "unknown" && !(a.Status == "accepted" && a.Result == "" && legacy.Version == 2) {
			continue
		}
		if a.ID == "" || !identifier.MatchString(a.RuleID) || seen[a.ID] {
			return errors.New("invalid saved native delivery; original data preserved")
		}
		seen[a.ID] = true
		id := legacyPlanID(a.RuleID)
		if !plans[id] {
			appendRetiredRule(&next, plans, a.RuleID, now)
		}
		at := a.DispatchedAt
		if at.IsZero() {
			at = now
		}
		state := "unknown_wake"
		if a.Status == "accepted" {
			state = "completed"
		}
		o := Occurrence{ID: a.ID, Plan: id, Event: id, At: at, Status: state}
		if state == "completed" {
			o.Finished = now
		}
		next.Occurrences = append(next.Occurrences, o)
	}
	if len(next.Plans) > 1024 || len(next.Occurrences) > 1024 {
		return errors.New("migration exceeds bounded storage")
	}
	next.Migration = &Migration{Version: 1, Digest: fmt.Sprintf("%x", sha256.Sum256(raw)), Completed: now}
	e.state = next
	return e.persistAs(writeObservation)
}

func legacyPlanID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return fmt.Sprintf("native-%x", sum[:28])
}

func appendRetiredRule(next *State, plans map[string]bool, rule string, now time.Time) bool {
	id := legacyPlanID(rule)
	if plans[id] {
		return false
	}
	plans[id] = true
	next.Plans = append(next.Plans, Plan{ID: id, Event: id, GrantID: "care:" + rule, Status: "cancelled", Created: now, Next: now, Finished: now})
	return true
}
