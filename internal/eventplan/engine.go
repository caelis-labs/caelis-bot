package eventplan

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/care"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type completion struct {
	id     string
	output json.RawMessage
	err    error
}
type Engine struct {
	workers    sync.WaitGroup
	mu         sync.Mutex
	state      State
	path       string
	write      func(string, any) error
	runner     Runner
	results    chan completion
	running    string
	cancel     context.CancelFunc
	stopped    bool
	fault      error
	lastStart  time.Time
	lastRevoke time.Time
}

func Open(path string, run Runner, now time.Time) (*Engine, error) {
	e := &Engine{path: path, write: localstate.Write, runner: run, results: make(chan completion, 1), state: State{Version: 1}}
	var interrupted []Occurrence
	raw, err := os.ReadFile(path)
	if err == nil {
		if len(raw) > 16<<20 || json.Unmarshal(raw, &e.state) != nil || e.state.Version != 1 {
			return nil, errors.New("event arrangements unavailable; preserve saved state")
		}
		for i := range e.state.Occurrences {
			o := &e.state.Occurrences[i]
			if o.Status == "collecting" {
				o.Status = "unknown"
				o.Issue = "collection_unconfirmed"
				interrupted = append(interrupted, *o)
			}
			if o.Status == "dispatching" {
				o.Status = "unknown_wake"
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// Validate definitions before any restored executable or expression is used.
	seen := map[string]bool{}
	for _, v := range e.state.Events {
		if seen[v.ID] || validateEvent(v) != nil {
			return nil, errors.New("invalid saved event definition")
		}
		seen[v.ID] = true
	}
	plans := map[string]bool{}
	for _, p := range e.state.Plans {
		if plans[p.ID] || p.Created.IsZero() || p.Expires.IsZero() || p.Next.IsZero() || p.CooldownSeconds < 60 || p.CooldownSeconds > 31622400 || !slices.Contains([]string{"active", "suspended", "cancelled", "expired", "filtered", "failed", "completed"}, p.Status) {
			return nil, errors.New("invalid saved arrangement state")
		}
		plans[p.ID] = true
		if p.Kind == "interval" && (p.Seconds < 60 || p.Seconds > 604800) || p.Kind == "completion" && p.Seconds != 30 || p.Kind == "after" && (p.Seconds < 1 || p.Seconds > 31622400) {
			return nil, errors.New("invalid saved timing")
		}
		if !identifier.MatchString(p.ID) || !seen[p.Event] || (p.Kind != "at" && p.Kind != "after" && p.Kind != "interval" && p.Kind != "completion") {
			return nil, errors.New("invalid saved arrangement")
		}
		if err := validateCondition(p); err != nil {
			return nil, errors.New("invalid saved condition")
		}
	}
	occurrences := map[string]bool{}
	for _, o := range e.state.Occurrences {
		if occurrences[o.ID] || !strings.HasPrefix(o.ID, "occurrence:") || !plans[o.Plan] || !seen[o.Event] || o.At.IsZero() || !slices.Contains([]string{"collecting", "unknown", "unknown_wake", "filtered", "pending", "dispatching", "accepted", "completed", "expired", "failed", "cancelled"}, o.Status) {
			return nil, errors.New("invalid saved occurrence")
		}
		occurrences[o.ID] = true
	}
	if len(e.state.Plans) > 512 || len(e.state.Events) > 512 || len(e.state.Occurrences) > 512 {
		return nil, errors.New("event state exceeds bounded capacity")
	}
	for _, o := range interrupted {
		if p := e.plan(o.Plan); p != nil && p.Finished.IsZero() {
			p.Status = "suspended"
			p.Issue = "collection_unconfirmed"
			e.attention(o, now)
		}
	}
	e.cleanup(now)
	if err = e.persist(); err != nil {
		return nil, err
	}
	return e, nil
}
func (e *Engine) persist() error {
	if err := e.write(e.path, e.state); err != nil {
		e.fault = err
		return err
	}
	return nil
}
func (e *Engine) usable() error {
	if e.stopped {
		return errors.New("event scheduler stopped")
	}
	if e.fault != nil {
		return errors.New("event storage unavailable; preserve original receipts")
	}
	return nil
}
func clone[T any](v T) T          { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func (e *Engine) Snapshot() State { e.mu.Lock(); defer e.mu.Unlock(); return clone(e.state) }
func validateEvent(v Event) error {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > 32<<10 {
		return errors.New("event definition exceeds 32 KiB")
	}
	if !identifier.MatchString(v.ID) || strings.TrimSpace(v.Label) == "" || len(v.Label) > 160 || len(v.Context) > 4096 {
		return errors.New("event needs a stable id, label and bounded context")
	}
	if err := ValidateSchema(v.OutputSchema); err != nil {
		return err
	}
	switch v.Kind {
	case "wake":
		if v.Script != "" || v.Tool != "" || v.Task != "" {
			return errors.New("wake event cannot execute a collector")
		}
	case "script":
		if v.Interpreter == "" || v.ExecutableDigest == "" || strings.TrimSpace(v.Script) == "" || len(v.Script) > 16384 || v.Tool != "" || v.Task != "" {
			return errors.New("script event requires a pinned interpreter and inline script")
		}
	case "tool":
		if v.Server == "" || v.Tool == "" || v.Binding == "" || v.Script != "" || v.Task != "" {
			return errors.New("tool event requires a pinned available server and tool")
		}
	case "task":
		if v.Task == "" || v.Script != "" || v.Tool != "" {
			return errors.New("task event requires original owned task handle")
		}
	default:
		return errors.New("unsupported event kind")
	}
	return nil
}
func (e *Engine) Register(v Event, now time.Time, authorize ...func() error) (Event, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.usable(); err != nil {
		return Event{}, err
	}
	e.cleanup(now)
	if err := validateEvent(v); err != nil {
		return Event{}, err
	}
	for _, old := range e.state.Events {
		if old.ID == v.ID {
			n := old
			n.Created = time.Time{}
			n.LastUsed = time.Time{}
			n.Archived = time.Time{}
			n.Retired = false
			if !old.Archived.IsZero() || !reflect.DeepEqual(clone(n), clone(v)) {
				return Event{}, errors.New("event id already bound; use a new id for a changed collector")
			}
			return old, nil
		}
	}
	if activeEvents(e.state) >= MaxActive || len(e.state.Events) >= MaxActive+MaxHistory {
		return Event{}, errors.New("active event limit reached; archive unused events")
	}
	if len(authorize) > 0 {
		if err := authorize[0](); err != nil {
			return Event{}, err
		}
	}
	v.Created = now
	v.LastUsed = now
	e.state.Events = append(e.state.Events, clone(v))
	return v, e.persist()
}
func activeEvents(s State) int {
	n := 0
	for _, v := range s.Events {
		if v.Archived.IsZero() {
			n++
		}
	}
	return n
}
func (e *Engine) event(id string) (Event, bool) {
	for _, v := range e.state.Events {
		if v.ID == id {
			return v, v.Archived.IsZero()
		}
	}
	return Event{}, false
}
func (e *Engine) Event(id string) (Event, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.event(id)
	if !ok {
		return v, errors.New("event unavailable")
	}
	return clone(v), nil
}
func Normalize(p Plan, now time.Time) (Plan, error) {
	if !identifier.MatchString(p.ID) || !identifier.MatchString(p.Event) {
		return p, errors.New("use a stable arrangement id and returned event handle")
	}
	if p.TimeZone == "" {
		p.TimeZone = "UTC"
	}
	if p.When == "" {
		p.When = "true"
	}
	if p.CooldownSeconds == 0 {
		p.CooldownSeconds = 3600
	}
	if p.CooldownSeconds < 60 || p.CooldownSeconds > 31622400 {
		return p, errors.New("cooldownSeconds must be 60–31622400")
	}
	switch p.Kind {
	case "after":
		if p.Seconds < 1 || p.Seconds > 31622400 {
			return p, errors.New("after seconds must be 1–31622400")
		}
		p.Next = now.Add(time.Duration(p.Seconds) * time.Second)
	case "interval":
		if p.Seconds < 60 || p.Seconds > 604800 {
			return p, errors.New("interval seconds must be 60–604800")
		}
		p.Next = now.Add(time.Duration(p.Seconds) * time.Second)
	case "completion":
		p.Seconds = 30
		p.Next = now
	case "at":
		t, err := time.Parse(time.RFC3339, p.At)
		if err != nil || !t.After(now) {
			return p, errors.New("at must be a future RFC3339 timestamp with offset")
		}
		p.Next = t
	default:
		return p, errors.New("unsupported arrangement type")
	}
	p.Expires = p.Next.Add(time.Hour)
	if p.Kind == "interval" || p.Kind == "completion" {
		p.Expires = now.Add(30 * 24 * time.Hour)
	}
	if p.Until != "" {
		t, err := time.Parse(time.RFC3339, p.Until)
		if err != nil || !t.After(now) || t.Before(p.Next) {
			return p, errors.New("until must be after the first check")
		}
		p.Expires = t
	}
	if p.Expires.After(now.Add(366 * 24 * time.Hour)) {
		return p, errors.New("arrangement lifetime exceeds one year")
	}
	// Pure compilation is independent of sample field availability.
	err := validateCondition(p)
	if err != nil {
		return p, err
	}
	p.Created = now
	p.Status = "active"
	return p, nil
}
func validateCondition(p Plan) error {
	when := p.When
	if when == "" {
		when = "true"
	}
	zone := p.TimeZone
	if zone == "" {
		zone = "UTC"
	}
	return care.Validate(care.Rule{ID: "event-test", Label: "Event condition", Prompt: "Check event", On: "registered.event", When: when, TimeZone: zone})
}
func Test(p Plan, data map[string]any, now time.Time) (bool, error) {
	when := p.When
	if when == "" {
		when = "true"
	}
	zone := p.TimeZone
	if zone == "" {
		zone = "UTC"
	}
	return care.Test(context.Background(), care.Rule{ID: "event-test", Label: "Event condition", Prompt: "Check event", On: "registered.event", When: when, TimeZone: zone}, data, now)
}
func definition(p Plan) Plan {
 p.OriginRequests = nil // retries retain the original registration source
	p.Created = time.Time{}
	p.Next = time.Time{}
	p.Expires = time.Time{}
	p.LastWake = time.Time{}
	p.Status = ""
	p.Issue = ""
	p.Finished = time.Time{}
	p.Failures = 0
	p.Retired = false
	return p
}
func (e *Engine) Save(p Plan, now time.Time, authorize ...func() error) (Plan, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.usable(); err != nil {
		return p, err
	}
	e.cleanup(now)
	// Identical retries preserve relative deadlines, even after one-shot completion.
	for _, old := range e.state.Plans {
		if old.ID == p.ID {
			n := p
			if n.TimeZone == "" {
				n.TimeZone = "UTC"
			}
			if n.When == "" {
				n.When = "true"
			}
			if n.CooldownSeconds == 0 {
				n.CooldownSeconds = 3600
			}
			if n.Kind == "completion" {
				n.Seconds = 30
			}
			if reflect.DeepEqual(definition(old), definition(n)) {
				return old, nil
			}
			return p, errors.New("arrangement id already bound; remove it and use a new id")
		}
	}
	v, ok := e.event(p.Event)
	if !ok {
		return p, errors.New("register the event first")
	}
	if p.Kind == "completion" && v.Kind != "task" {
		return p, errors.New("completion requires an owned-task event")
	}
	var err error
	p, err = Normalize(p, now)
	if err != nil {
		return p, err
	}
	active := 0
	for _, s := range e.state.Plans {
		if s.Finished.IsZero() || e.pending(s.ID) {
			active++
		}
	}
	if active >= MaxActive || len(e.state.Plans) >= MaxActive+MaxHistory {
		return p, errors.New("active arrangement limit reached")
	}
	if len(authorize) > 0 {
		if err := authorize[0](); err != nil {
			return p, err
		}
	}
	e.state.Plans = append(e.state.Plans, p)
	for i := range e.state.Events {
		if e.state.Events[i].ID == v.ID {
			e.state.Events[i].LastUsed = now
		}
	}
	return p, e.persist()
}
func unresolved(o Occurrence) bool { return o.Finished.IsZero() }
func (e *Engine) pending(id string) bool {
	return slices.ContainsFunc(e.state.Occurrences, func(o Occurrence) bool { return o.Plan == id && unresolved(o) })
}
func (e *Engine) Remove(id string, now time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.usable(); err != nil {
		return err
	}
	found := false
	for i := range e.state.Plans {
		p := &e.state.Plans[i]
		if p.ID == id {
			p.Status = "cancelled"
			p.Finished = now
			found = true
		}
	}
	if !found {
		return errors.New("arrangement not found")
	}
	for i := range e.state.Occurrences {
		o := &e.state.Occurrences[i]
		if o.Plan == id && o.Status == "pending" {
			o.Status = "cancelled"
			o.Finished = now
		}
	}
	return e.persist()
}
func (e *Engine) ArchiveEvent(id string, now time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.usable(); err != nil {
		return err
	}
	for _, p := range e.state.Plans {
		if p.Event == id && (p.Finished.IsZero() || e.pending(p.ID)) {
			return errors.New("event still has active arrangements or unresolved receipts")
		}
	}
	for i := range e.state.Events {
		v := &e.state.Events[i]
		if v.ID == id {
			v.Archived = now
			return e.persist()
		}
	}
	return errors.New("event not found")
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
