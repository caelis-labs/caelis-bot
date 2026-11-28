package eventplan

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/scheduledata"
)

var ErrObservationUnavailable = errors.New("Notebook Shell observation unavailable")

var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type completion struct {
	id     string
	output json.RawMessage
	err    error
}

type Engine struct {
	workers      sync.WaitGroup
	mu           sync.Mutex
	state        State
	saved        State
	writing      bool
	retry        *State
	path         string
	write        func(string, any) error
	runner       Runner
	results      chan completion
	changes      chan struct{}
	running      string
	cancel       context.CancelFunc
	stopped      bool
	firstFailure error
	lastRevoke   time.Time
}

func Open(path string, run Runner, now time.Time) (*Engine, error) {
	e := &Engine{path: path, write: localstate.Write, runner: run, results: make(chan completion, 1), changes: make(chan struct{}, 1), state: State{Version: 2}}
	raw, err := localstate.ReadAfterWrites(path)
	changed := errors.Is(err, os.ErrNotExist)
	switch {
	case err == nil:
		if len(raw) > 16<<20 || json.Unmarshal(raw, &e.state) != nil || (e.state.Version != 1 && e.state.Version != 2) {
			return nil, errors.New("event arrangements unavailable; preserve saved state")
		}
	case !changed:
		return nil, err
	}
	e.saved = copyState(e.state)
	if e.state.Version == 1 {
		e.upgrade(raw, now)
		changed = true
	}
	if err := e.validateSaved(); err != nil {
		return nil, err
	}
	for i := range e.state.Occurrences {
		o := &e.state.Occurrences[i]
		switch o.Status {
		case "collecting":
			o.Status, o.Issue = "unknown", "collection_unconfirmed"
			if p := e.plan(o.Plan); p.Finished.IsZero() {
				p.Status, p.Issue = "suspended", o.Issue
			}
			changed = true
		case "dispatching":
			o.Status = "unknown_wake"
			changed = true
		}
	}
	if changed {
		// No admission until interrupted identities and schema changes are durable.
		e.mu.Lock()
		_ = e.persistAs(writeObservation)
		e.mu.Unlock()
	}
	return e, nil
}

func (e *Engine) validateSaved() error {
	if len(e.state.Plans) > 1024 || len(e.state.Events) > 1024 || len(e.state.Occurrences) > 1024 {
		return errors.New("event state exceeds bounded capacity")
	}
	if m := e.state.Migration; m != nil && (m.Version != 1 || m.Digest == "" || m.Completed.IsZero()) {
		return errors.New("unsupported native migration checkpoint")
	}
	events := map[string]bool{}
	for _, v := range e.state.Events {
		if events[v.ID] || validateEvent(v) != nil {
			return errors.New("invalid saved event definition")
		}
		events[v.ID] = true
	}
	plans := map[string]Plan{}
	for _, p := range e.state.Plans {
		_, duplicate := plans[p.ID]
		if duplicate || !identifier.MatchString(p.ID) || !identifier.MatchString(p.Event) || p.Created.IsZero() ||
			!slices.Contains([]string{"active", "suspended", "cancelled", "expired", "filtered", "failed", "completed"}, p.Status) {
			return errors.New("invalid saved arrangement state")
		}
		if p.Finished.IsZero() {
			if p.Next.IsZero() || !events[p.Event] || !validTiming(p) || validateCondition(p) != nil {
				return errors.New("invalid active arrangement")
			}
		}
		plans[p.ID] = p
	}
	seen := map[string]bool{}
	for _, o := range e.state.Occurrences {
		p, ok := plans[o.Plan]
		if seen[o.ID] || !ok || p.Event != o.Event || o.At.IsZero() || o.ID == "" ||
			(!strings.HasPrefix(o.ID, "occurrence:") && e.state.Migration == nil) ||
			!slices.Contains([]string{"collecting", "unknown", "unknown_wake", "filtered", "pending", "dispatching", "accepted", "completed", "expired", "failed", "cancelled"}, o.Status) {
			return errors.New("invalid saved occurrence")
		}
		seen[o.ID] = true
	}
	return nil
}

func (e *Engine) saveChanges() error {
	defer e.signal()
	return e.persist()
}

func (e *Engine) signal() {
	select {
	case e.changes <- struct{}{}:
	default:
	}
}

func (e *Engine) Changes() <-chan struct{} { return e.changes }

func (e *Engine) FirstStorageFailure() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.firstFailure
}

func (e *Engine) prepareWrite() error {
	if e.stopped {
		return errors.New("event scheduler stopped")
	}
	if e.writing {
		return ErrWritePending
	}
	return e.retryWrite()
}

func clone[T any](v T) T {
	b, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(b, &out)
	return out
}
func (e *Engine) Snapshot() State { e.mu.Lock(); defer e.mu.Unlock(); return clone(e.state) }

func validateEvent(v Event) error {
	if !identifier.MatchString(v.ID) || strings.TrimSpace(v.Label) == "" || len(v.Label) > 200 || len(v.Context) > 4096 {
		return errors.New("event needs a stable id, label and bounded context")
	}
	switch v.Kind {
	case "wake":
		if v.Command == "" {
			return nil
		}
	case "shell":
		if strings.TrimSpace(v.Command) != "" && len(v.Command) <= 4096 && !strings.ContainsRune(v.Command, 0) {
			return nil
		}
	}
	return errors.New("invalid observation definition")
}

func (e *Engine) event(id string) (Event, bool) {
	for _, v := range e.state.Events {
		if v.ID == id {
			return v, true
		}
	}
	return Event{}, false
}

func validTiming(p Plan) bool {
	switch p.Kind {
	case "after":
		return p.Seconds >= 1 && p.Seconds <= 31622400
	case "interval":
		return p.Seconds >= 60 && p.Seconds <= 604800
	case "at":
		_, err := time.Parse(time.RFC3339, p.At)
		return err == nil
	}
	return false
}

func normalizeCondition(p Plan) Plan {
	if p.TimeZone == "" {
		p.TimeZone = "UTC"
	}
	if p.When == "" {
		p.When = "true"
	}
	return p
}

func validateCondition(p Plan) error {
	p = normalizeCondition(p)
	return scheduledata.Validate(p.When, p.TimeZone)
}

func testCondition(p Plan, data map[string]any, now time.Time) (bool, error) {
	p = normalizeCondition(p)
	return scheduledata.Test(context.Background(), p.When, p.TimeZone, data, now)
}

func scheduleTimes(p Plan, v Event, now time.Time) (Plan, error) {
	if !validTiming(p) {
		return p, errors.New("invalid schedule timing")
	}
	p.Next = now.Add(time.Duration(p.Seconds) * time.Second)
	if p.Kind == "at" {
		p.Next, _ = time.Parse(time.RFC3339, p.At)
		if !p.Next.After(now) {
			return p, errors.New("at must be a future RFC3339 timestamp with offset")
		}
	}
	if p.Kind != "interval" && v.Kind == "wake" {
		p.Expires = p.Next.Add(time.Hour)
	}
	if p.Until != "" {
		t, err := time.Parse(time.RFC3339, p.Until)
		if err != nil || !t.After(now) || !t.After(p.Next) || t.After(now.Add(366*24*time.Hour)) {
			return p, errors.New("until must be after the first check and within one year")
		}
		p.Expires = t
	}
	return p, nil
}

func fingerprint(p Plan, inline *Event) string {
	p = normalizeCondition(p)
	b, _ := json.Marshal(struct {
		ID, Event, Kind, At, When, TimeZone, Until string
		Seconds                                    int
		Inline                                     *Event
	}{p.ID, p.Event, p.Kind, p.At, p.When, p.TimeZone, p.Until, p.Seconds, inline})
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// Save atomically saves a timed arrangement and its inline definition, or reuses
// an existing definition by handle. Retries retain the original schedule ID.
func (e *Engine) Save(p Plan, inline *Event, now time.Time, shellAvailable func() bool) (Plan, error) {
	if inline != nil {
		p.Event = inline.ID
		if err := validateEvent(*inline); err != nil {
			return p, err
		}
	}
	if !identifier.MatchString(p.ID) || !identifier.MatchString(p.Event) {
		return p, errors.New("use a stable arrangement id and a valid observation handle")
	}
	p.Fingerprint = fingerprint(p, inline)
	e.mu.Lock()
	candidate, definition, existing, err := e.checkSave(p, inline, now)
	if err == nil && !existing && definition.Kind == "shell" && shellAvailable != nil {
		// A Control handshake may wait on another process. Revalidate against
		// the current ledger before committing the checked registration.
		e.mu.Unlock()
		available := shellAvailable()
		e.mu.Lock()
		var current Event
		candidate, current, existing, err = e.checkSave(p, inline, now)
		if err == nil && !existing {
			switch {
			case current != definition:
				err = errors.New("observation changed during registration")
			case !available:
				err = ErrObservationUnavailable
			}
		}
	}
	defer e.mu.Unlock()
	if err != nil || existing {
		return candidate, err
	}
	if inline != nil {
		definition.Created = now
		if definition.Kind == "shell" {
			definition.When, definition.TimeZone = candidate.When, candidate.TimeZone
		}
		e.state.Events = append(e.state.Events, definition)
	}
	candidate.OriginRequests = slices.Clone(candidate.OriginRequests)
	candidate.Created, candidate.Status = now, "active"
	e.state.Plans = append(e.state.Plans, candidate)
	return candidate, e.saveChanges()
}

func (e *Engine) checkSave(p Plan, inline *Event, now time.Time) (Plan, Event, bool, error) {
	if err := e.prepareWrite(); err != nil {
		return p, Event{}, false, err
	}
	if old := e.plan(p.ID); old != nil {
		if old.Fingerprint == p.Fingerprint {
			return *old, Event{}, true, nil
		}
		return p, Event{}, false, errors.New("arrangement id already bound; use a new id")
	}
	if e.activePlans() >= MaxActive || len(e.state.Plans) >= 1024 {
		return p, Event{}, false, errors.New("active arrangement limit reached")
	}
	v, exists := e.event(p.Event)
	if inline != nil {
		if exists {
			return p, Event{}, false, errors.New("observation id already bound; reference it or use a new id")
		}
		if len(e.state.Events) >= MaxActive {
			return p, Event{}, false, errors.New("observation definition limit reached")
		}
		v = *inline
	} else if !exists {
		return p, Event{}, false, errors.New("observation unavailable")
	}
	if v.Kind == "shell" {
		if p.When == "" {
			p.When = v.When
			if p.TimeZone == "" {
				p.TimeZone = v.TimeZone
			}
		}
		if strings.TrimSpace(p.When) == "" {
			return p, Event{}, false, errors.New("Shell observation requires a wake condition")
		}
	}
	p = normalizeCondition(p)
	p, err := scheduleTimes(p, v, now)
	if err == nil {
		err = validateCondition(p)
	}
	return p, v, false, err
}

func (e *Engine) activePlans() int {
	n := 0
	for _, p := range e.state.Plans {
		if p.Finished.IsZero() || e.pending(p.ID) {
			n++
		}
	}
	return n
}

func (e *Engine) pending(id string) bool {
	return slices.ContainsFunc(e.state.Occurrences, func(o Occurrence) bool { return o.Plan == id && o.Finished.IsZero() })
}

func (e *Engine) Remove(id string, now time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.prepareWrite(); err != nil {
		return err
	}
	p := e.plan(id)
	if p == nil {
		return errors.New("arrangement not found")
	}
	if p.Finished.IsZero() {
		p.Status, p.Finished = "cancelled", now
	}
	for i := range e.state.Occurrences {
		o := &e.state.Occurrences[i]
		if o.Plan == id && o.Status == "pending" {
			o.Status, o.Finished = "cancelled", now
		}
	}
	e.cleanup(now)
	return e.saveChanges()
}

func (e *Engine) RemoveEvent(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.prepareWrite(); err != nil {
		return err
	}
	if e.eventInUse(id) {
		return errors.New("observation still has active or unresolved arrangements")
	}
	before := len(e.state.Events)
	e.state.Events = slices.DeleteFunc(e.state.Events, func(v Event) bool { return v.ID == id })
	if len(e.state.Events) == before {
		return errors.New("observation unavailable")
	}
	return e.saveChanges()
}

func (e *Engine) Close() {
	e.mu.Lock()
	e.stopped = true
	if e.cancel != nil {
		e.cancel()
	}
	e.mu.Unlock()
	e.workers.Wait()
}
