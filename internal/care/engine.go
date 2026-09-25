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
	ID      string    `json:"id"`
	RuleID  string    `json:"ruleId"`
	Version string    `json:"version"`
	Prompt  string    `json:"prompt"`
	Expires time.Time `json:"expires"`
	Status  string    `json:"status"`
}
type State struct {
	Version     int                  `json:"version"`
	Rules       []Registration       `json:"rules"`
	Activations []Activation         `json:"activations"`
	Watermarks  map[string]time.Time `json:"watermarks"`
	Attempts    []time.Time          `json:"attempts"`
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
	e := &Engine{sources: catalog, path: path, write: durableWrite, programs: map[string]condition{}, state: State{Version: 1, Rules: []Registration{}, Activations: []Activation{}, Watermarks: map[string]time.Time{}, Attempts: []time.Time{}}}
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
			return nil, errors.New("invalid care state file")
		}
		if json.NewDecoder(f).Decode(&e.state) != nil || e.state.Version != 1 || e.state.Watermarks == nil || len(e.state.Rules) > MaxRules || len(e.state.Activations) > 256 || len(e.state.Attempts) > 8 {
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
	for i := range e.state.Activations {
		a := &e.state.Activations[i]
		if a.ID == "" || a.Version == "" || !slices.Contains([]string{"pending", "dispatching", "unknown", "accepted", "rejected", "expired", "cancelled"}, a.Status) {
			return nil, errors.New("invalid activation record")
		}
		if a.Status == "dispatching" {
			a.Status = "unknown"
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
		if !e.hasSource(r.On) {
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
	if !e.hasSource(r.On) {
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
		if !r.Enabled || r.On != event.Source {
			continue
		}
		interested = true
		if !r.Last.IsZero() && now.Before(r.Last.Add(time.Duration(r.CooldownSeconds)*time.Second)) {
			continue
		}
		if slices.ContainsFunc(next.Activations, func(a Activation) bool {
			return a.RuleID == r.ID && (a.Status == "pending" || a.Status == "unknown" || a.Status == "dispatching")
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
			if a.Status == "pending" || a.Status == "unknown" || a.Status == "dispatching" {
				active++
			}
		}
		if active >= MaxRules {
			next.Rules[i].Issue = "queue_full"
			continue
		}
		next.Activations = append(next.Activations, Activation{ID: "care-" + rand.Text(), RuleID: r.ID, Version: r.Version, Prompt: r.Prompt, Expires: now.Add(time.Duration(r.ExpiresSeconds) * time.Second), Status: "pending"})
		next.Rules[i].Last = now
	}
	if !interested {
		return nil
	}
	next.Watermarks[event.Source] = event.At
	trim(&next, now)
	return e.commit(next)
}
func trim(s *State, now time.Time) {
	s.Attempts = slices.DeleteFunc(s.Attempts, func(t time.Time) bool { return !t.After(now.Add(-24 * time.Hour)) })
	for len(s.Activations) > 256 {
		index := slices.IndexFunc(s.Activations, func(a Activation) bool {
			return a.Status != "pending" && a.Status != "unknown" && a.Status != "dispatching"
		})
		if index < 0 {
			break
		}
		s.Activations = slices.Delete(s.Activations, index, index+1)
	}
}

// Deliver submits at most one activation. Unknown outcomes block later dispatch
// until native receipts resolve them; no timer, retry or restart grants certainty.
func (e *Engine) Deliver(ctx context.Context, now time.Time, p Presence, canSend bool, lookup func(string) api.Receipt, send func(context.Context, Activation) (api.Receipt, error)) error {
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
	unresolved := false
	for i, a := range next.Activations {
		if a.Status == "unknown" || a.Status == "dispatching" {
			r := lookup(a.ID)
			if r.ID == a.ID && (r.Outcome == "accepted" || r.Outcome == "rejected") {
				next.Activations[i].Status = r.Outcome
			} else {
				unresolved = true
			}
		}
		if a.Status == "pending" {
			current := slices.ContainsFunc(next.Rules, func(r Registration) bool {
				return r.Enabled && e.hasSource(r.On) && r.ID == a.RuleID && r.Version == a.Version
			})
			if !current {
				next.Activations[i].Status = "cancelled"
			} else if !now.Before(a.Expires) {
				next.Activations[i].Status = "expired"
			}
		}
	}
	if !reflect.DeepEqual(next, e.state) {
		if err := e.commit(next); err != nil {
			e.mu.Unlock()
			return err
		}
	}
	if unresolved || !canSend || !p.Available() || len(next.Attempts) >= 8 || len(next.Attempts) > 0 && now.Before(next.Attempts[len(next.Attempts)-1].Add(5*time.Minute)) {
		e.mu.Unlock()
		return nil
	}
	index := slices.IndexFunc(next.Activations, func(a Activation) bool { return a.Status == "pending" })
	if index < 0 {
		e.mu.Unlock()
		return nil
	}
	next.Activations[index].Status = "dispatching"
	next.Attempts = append(next.Attempts, now)
	if err := e.commit(next); err != nil {
		e.mu.Unlock()
		return err
	}
	a := next.Activations[index]
	e.mu.Unlock()
	receipt, err := send(ctx, a)
	e.mu.Lock()
	defer e.mu.Unlock()
	next = copyState(e.state)
	status := "unknown"
	// An exact native receipt remains authoritative when an adapter also returns
	// explanatory errors (for example, an explicitly revoked background grant).
	if receipt.ID == a.ID && (receipt.Outcome == "accepted" || receipt.Outcome == "rejected") {
		status = receipt.Outcome
	}
	next.Activations[index].Status = status
	if save := e.commit(next); save != nil {
		return save
	}
	return err
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
