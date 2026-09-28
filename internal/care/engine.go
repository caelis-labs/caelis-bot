package care

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

type Registration struct {
	Rule
	Version string    `json:"version"`
	Enabled bool      `json:"enabled"`
	Last    time.Time `json:"last,omitempty"`
	Issue   string    `json:"issue,omitempty"`
}
type Activation struct {
	ID           string    `json:"id"`
	RuleID       string    `json:"ruleId"`
	Version      string    `json:"version"`
	Prompt       string    `json:"prompt"`
	Expires      time.Time `json:"expires"`
	Status       string    `json:"status"`
	Source       string    `json:"source,omitempty"`
	DispatchedAt time.Time `json:"dispatchedAt,omitempty"`
	VisibleAt    time.Time `json:"visibleAt,omitempty"`
	Result       string    `json:"result,omitempty"` // visible, silent, or legacy (v1 attempt accounting)
}
type State struct {
	Version        int                  `json:"version"`
	Rules          []Registration       `json:"rules"`
	Activations    []Activation         `json:"activations"`
	Watermarks     map[string]time.Time `json:"watermarks"`
	Attempts       []time.Time          `json:"attempts"`
	Policy         Policy               `json:"policy"`
	Interruptions  []time.Time          `json:"interruptions"`
	LegacyAttempts []time.Time          `json:"legacyAttempts,omitempty"`
}
type Presence struct {
	Awake    bool
	Unlocked *bool
}

func (p Presence) Available() bool { return p.Awake && p.Unlocked != nil && *p.Unlocked }

type Engine struct {
	sources  map[string]Source
	op       sync.Mutex
	mu       sync.Mutex
	path     string
	state    State
	programs map[string]condition
	fatal    error
	write    func(string, any) error
}

// The colon cannot occur in reminder IDs, so their grants cannot collide.
func GrantID(id string) string { return "care:" + id }
func durableWrite(path string, value any) error {
	if err := localstate.Write(path, value); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func Open(path string) (*Engine, error) { return OpenWithSources(path, NativeSources()) }
func OpenWithSources(path string, sources []Source) (*Engine, error) {
	catalog, err := registry(sources)
	if err != nil {
		return nil, err
	}
	e := &Engine{sources: catalog, path: path, write: durableWrite, programs: map[string]condition{}, state: State{Version: 2, Policy: DefaultPolicy(), Interruptions: []time.Time{}, Rules: []Registration{}, Activations: []Activation{}, Watermarks: map[string]time.Time{}, Attempts: []time.Time{}}}
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
			return nil, errors.New("invalid care state file")
		}
		if json.NewDecoder(f).Decode(&e.state) != nil || (e.state.Version != 1 && e.state.Version != 2) || e.state.Watermarks == nil || len(e.state.Rules) > MaxRules || len(e.state.Activations) > 256 || len(e.state.Attempts) > 256 || (e.state.Version == 1 && len(e.state.Attempts) > 8) || !e.state.Policy.valid() || len(e.state.Interruptions) > 256 || len(e.state.LegacyAttempts) > 8 {
			return nil, errors.New("invalid care state")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, r := range e.state.Rules {
		normalized, c, err := compile(r.Rule)
		if err != nil || r.Version == "" || !reflect.DeepEqual(normalized, r.Rule) {
			return nil, errors.New("invalid saved care rule")
		}
		if _, exists := e.programs[r.ID]; exists {
			return nil, errors.New("duplicate saved care rule")
		}
		e.programs[r.ID] = c
	}
	legacy := e.state.Version == 1
	if legacy {
		e.state.LegacyAttempts = slices.Clone(e.state.Attempts)
		e.state.Version = 2
	}
	seen := map[string]bool{}
	for i := range e.state.Activations {
		a := &e.state.Activations[i]
		if a.ID == "" || a.Version == "" || seen[a.ID] || !slices.Contains([]string{"", "silent", "visible", "legacy"}, a.Result) || !slices.Contains([]string{"pending", "dispatching", "unknown", "accepted", "rejected", "expired", "cancelled"}, a.Status) {
			return nil, errors.New("invalid activation record")
		}
		seen[a.ID] = true
		if legacy && a.Status == "accepted" {
			a.Result = "legacy"
		}
		if a.Status == "dispatching" {
			a.Status = "unknown"
		}
	}
	if legacy {
		if err := e.commit(e.state); err != nil {
			return nil, err
		}
	}
	return e, nil
}
func copyState(s State) State {
	b, _ := json.Marshal(s)
	var out State
	_ = json.Unmarshal(b, &out)
	return out
}
func (e *Engine) Snapshot() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := copyState(e.state)
	for i, r := range out.Rules {
		if !e.hasAllSources(r.Rule) {
			out.Rules[i].Issue = "source_unavailable"
		}
	}
	return out
}
func (e *Engine) commit(s State) error {
	if e.fatal != nil {
		return e.fatal
	}
	// A write error after rename is uncertain. Freeze the instance rather than
	// rolling back memory and risking another dispatch from a stale state.
	e.state = s
	if err := e.write(e.path, s); err != nil {
		e.fatal = errors.New("care persistence failed; reconnect after repairing storage")
		return e.fatal
	}
	return nil
}
func cancelPending(s *State, id string) {
	for i := range s.Activations {
		if s.Activations[i].RuleID == id && s.Activations[i].Status == "pending" {
			s.Activations[i].Status = "cancelled"
		}
	}
}

// Save validates before authorization. A replacement is disabled and its queued
// work cancelled durably before changing a native grant. Same-intent retries
// retain the candidate version, including a lost grant response.
func (e *Engine) Save(ctx context.Context, r Rule, authorize func(context.Context, string, string) error) (Registration, error) {
	r, c, err := compile(r)
	if !e.hasAllSources(r) {
		return Registration{}, errors.New("unsupported event source")
	}
	if err != nil {
		return Registration{}, err
	}
	e.op.Lock()
	defer e.op.Unlock()
	if err := ctx.Err(); err != nil {
		return Registration{}, err
	}
	e.mu.Lock()
	if e.fatal != nil {
		err = e.fatal
		e.mu.Unlock()
		return Registration{}, err
	}
	next := copyState(e.state)
	index := slices.IndexFunc(next.Rules, func(v Registration) bool { return v.ID == r.ID })
	record := Registration{Rule: r, Version: rand.Text(), Issue: "authorization_pending"}
	if index >= 0 && reflect.DeepEqual(next.Rules[index].Rule, r) {
		record = next.Rules[index]
		if record.Enabled {
			e.mu.Unlock()
			return record, nil
		}
	}
	if index < 0 {
		if len(next.Rules) >= MaxRules {
			e.mu.Unlock()
			return record, errors.New("at most 32 care rules")
		}
		next.Rules = append(next.Rules, record)
		index = len(next.Rules) - 1
	} else {
		next.Rules[index] = record
	}
	cancelPending(&next, r.ID)
	err = e.commit(next)
	e.programs[r.ID] = c
	e.mu.Unlock()
	if err != nil {
		return record, err
	}
	raw, _ := json.Marshal(struct {
		Rule    Rule
		Version string
	}{r, record.Version})
	if authorize != nil {
		if err = authorize(ctx, GrantID(r.ID), string(raw)); err != nil {
			return record, err
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	next = copyState(e.state)
	record.Enabled = true
	record.Issue = ""
	next.Rules[index] = record
	return record, e.commit(next)
}
func (e *Engine) Remove(ctx context.Context, id string, revoke func(context.Context, string) error) error {
	if !identifier.MatchString(id) {
		return errors.New("invalid rule id")
	}
	e.op.Lock()
	defer e.op.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	next := copyState(e.state)
	index := slices.IndexFunc(next.Rules, func(v Registration) bool { return v.ID == id })
	if index < 0 {
		e.mu.Unlock()
		return nil
	}
	next.Rules[index].Enabled = false
	next.Rules[index].Issue = "removal_pending"
	cancelPending(&next, id)
	err := e.commit(next)
	e.mu.Unlock()
	if err != nil {
		return err
	}
	if revoke != nil {
		if err = revoke(ctx, GrantID(id)); err != nil {
			return err
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	next = copyState(e.state)
	next.Rules = slices.Delete(next.Rules, index, index+1)
	delete(e.programs, id)
	return e.commit(next)
}
func (e *Engine) Receive(ctx context.Context, event Event, now time.Time) error {
	if !e.hasSource(event.Source) || event.At.IsZero() || event.At.After(now.Add(5*time.Second)) || now.Sub(event.At) > 5*time.Minute {
		return errors.New("unsupported, stale or future event")
	}
	data, err := eventData(event)
	if err != nil {
		return err
	}
	e.op.Lock()
	defer e.op.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fatal != nil {
		return e.fatal
	}
	if !event.At.After(e.state.Watermarks[event.Source]) {
		return nil
	}
	next := copyState(e.state)
	interested := false
	for i, r := range next.Rules {
		if !r.Enabled || !r.subscribes(event.Source) {
			continue
		}
		interested = true
		if !r.Last.IsZero() && now.Before(r.Last.Add(time.Duration(r.CooldownSeconds)*time.Second)) {
			continue
		}
		if slices.ContainsFunc(next.Activations, func(a Activation) bool {
			return a.RuleID == r.ID && (a.Status == "pending" || inFlight(a))
		}) {
			continue
		}
		matched, err := e.programs[r.ID].evaluate(ctx, data, now)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			next.Rules[i].Issue = "condition_error"
			continue
		}
		next.Rules[i].Issue = ""
		if !matched {
			continue
		}
		active := 0
		for _, a := range next.Activations {
			if a.Status == "pending" || inFlight(a) {
				active++
			}
		}
		if active >= MaxRules {
			next.Rules[i].Issue = "queue_full"
			continue
		}
		next.Activations = append(next.Activations, Activation{ID: "care-" + rand.Text(), RuleID: r.ID, Version: r.Version, Prompt: r.Prompt, Source: event.Source, Expires: now.Add(time.Duration(r.ExpiresSeconds) * time.Second), Status: "pending"})
	}
	if !interested {
		return nil
	}
	next.Watermarks[event.Source] = event.At
	trim(&next, now)
	return e.commit(next)
}
func trim(s *State, now time.Time) {
	expired := func(t time.Time) bool { return !t.After(now.Add(-24 * time.Hour)) }
	s.Attempts = slices.DeleteFunc(s.Attempts, expired)
	if len(s.Attempts) > 256 {
		s.Attempts = s.Attempts[len(s.Attempts)-256:]
	}
	s.Interruptions = slices.DeleteFunc(s.Interruptions, expired)
	s.LegacyAttempts = slices.DeleteFunc(s.LegacyAttempts, expired)
	for len(s.Activations) > 256 {
		index := slices.IndexFunc(s.Activations, func(a Activation) bool {
			return a.Status != "pending" && !inFlight(a)
		})
		if index < 0 {
			break
		}
		s.Activations = slices.Delete(s.Activations, index, index+1)
	}
}

// Deliver admits at most one occurrence. The native runtime still owns idle
// admission; an uncertain care receipt alone does not block unrelated rules.
// Optional result evidence is required to release accepted reservations.
func (e *Engine) Deliver(ctx context.Context, now time.Time, p Presence, canSend bool, lookup func(string) api.Receipt, send func(context.Context, Activation) (api.Receipt, error), results ...func(string) api.BackgroundResult) error {
	e.op.Lock()
	defer e.op.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	if e.fatal != nil {
		err := e.fatal
		e.mu.Unlock()
		return err
	}
	next := copyState(e.state)
	trim(&next, now)
	for i := range next.Activations {
		a := &next.Activations[i]
		if a.Status == "unknown" || a.Status == "dispatching" {
			receipt := lookup(a.ID)
			if receipt.ID == a.ID && (receipt.Outcome == "accepted" || receipt.Outcome == "rejected") {
				a.Status = receipt.Outcome
			}
		}
		if inFlight(*a) && len(results) > 0 && results[0] != nil {
			result := results[0](a.ID)
			if result.ID == a.ID {
				if result.Visible || result.Complete {
					a.Status = "accepted"
				}
				if result.Visible && a.VisibleAt.IsZero() {
					a.VisibleAt = result.ObservedAt
					if a.VisibleAt.IsZero() {
						a.VisibleAt = now
					}
					next.Interruptions = append(next.Interruptions, a.VisibleAt)
				}
				if result.Complete {
					a.Result = "silent"
					if !a.VisibleAt.IsZero() {
						a.Result = "visible"
					}
				}
			}
		}
		if a.Status == "rejected" {
			releaseCooldown(&next, *a)
		}
		if a.Status == "pending" {
			current := slices.ContainsFunc(next.Rules, func(r Registration) bool {
				return r.Enabled && e.hasAnySource(r.Rule) && (a.Source == "" || e.hasSource(a.Source)) && r.ID == a.RuleID && r.Version == a.Version
			})
			if !current {
				a.Status = "cancelled"
			} else if !now.Before(a.Expires) {
				a.Status = "expired"
			}
		}
	}
	trim(&next, now)
	if !reflect.DeepEqual(next, e.state) {
		if err := e.commit(next); err != nil {
			e.mu.Unlock()
			return err
		}
	}
	b := budget(next, now)
	if !canSend || !p.Available() || b.Remaining == 0 || len(next.Attempts) > 0 && now.Before(next.Attempts[len(next.Attempts)-1].Add(time.Duration(next.Policy.MinimumGapSeconds)*time.Second)) {
		e.mu.Unlock()
		return nil
	}
	index := slices.IndexFunc(next.Activations, func(a Activation) bool {
		return a.Status == "pending" && !slices.ContainsFunc(next.Activations, func(other Activation) bool { return other.RuleID == a.RuleID && inFlight(other) })
	})
	if index < 0 {
		e.mu.Unlock()
		return nil
	}
	a := &next.Activations[index]
	a.Status = "dispatching"
	a.DispatchedAt = now
	for i := range next.Rules {
		if next.Rules[i].ID == a.RuleID && next.Rules[i].Version == a.Version {
			next.Rules[i].Last = now
		}
	}
	dispatched := *a
	next.Attempts = append(next.Attempts, now)
	trim(&next, now)
	if err := e.commit(next); err != nil {
		e.mu.Unlock()
		return err
	}
	e.mu.Unlock()
	receipt, err := send(ctx, dispatched)
	e.mu.Lock()
	defer e.mu.Unlock()
	next = copyState(e.state)
	// trim may have shifted terminal records before the dispatch was persisted.
	index = slices.IndexFunc(next.Activations, func(a Activation) bool { return a.ID == dispatched.ID })
	status := "unknown"
	if receipt.ID == dispatched.ID && (receipt.Outcome == "accepted" || receipt.Outcome == "rejected") {
		status = receipt.Outcome
	}
	next.Activations[index].Status = status
	if status == "rejected" {
		releaseCooldown(&next, next.Activations[index])
	}
	if save := e.commit(next); save != nil {
		return save
	}
	return err
}

func inFlight(a Activation) bool {
	return a.Status == "unknown" || a.Status == "dispatching" || a.Status == "accepted" && a.Result == ""
}
func releaseCooldown(s *State, a Activation) {
	for i := range s.Rules {
		r := &s.Rules[i]
		if r.ID == a.RuleID && r.Version == a.Version && !a.DispatchedAt.IsZero() && r.Last.Equal(a.DispatchedAt) {
			r.Last = time.Time{}
		}
	}
}

func (e *Engine) Status() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fatal != nil {
		return e.fatal.Error()
	}
	for _, a := range e.state.Activations {
		if a.Status == "unknown" || a.Status == "dispatching" {
			return "主动关怀有一次发送结果待确认；不会自动重发。"
		}
	}
	return ""
}
