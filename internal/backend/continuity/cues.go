// Package continuity samples ordinary request inputs for occasional review
// invitations. It neither measures retained context nor authorizes renewal.
package continuity

import (
	"fmt"
	"slices"
	"time"
)

// These are low-frequency invitation heuristics, not calibrated cost limits.
const (
	MinAge               = 7 * 24 * time.Hour
	WindowWidth          = 24 * time.Hour
	MinSpan              = 48 * time.Hour
	Cooldown             = 7 * 24 * time.Hour
	Retention            = 28 * 24 * time.Hour
	MinWindows           = 4
	MinTurns             = 8
	TurnsPerWindow       = 64
	HighInput      int64 = 64_000
	GrowthInput    int64 = 16_000
)

type Sample struct {
	Counter int64     `json:"counter,omitempty"`
	Turn    string    `json:"turn"`
	At      time.Time `json:"at"`
	Input   int64     `json:"input"`
}
type Window struct {
	Start   time.Time `json:"start"`
	Samples []Sample  `json:"samples"`
}
type Pending struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}
type State struct {
	Binding       string    `json:"binding,omitempty"`
	Since         time.Time `json:"since,omitempty"`
	Created       time.Time `json:"created,omitempty"`
	Configuration string    `json:"configuration,omitempty"`
	Windows       []Window  `json:"windows,omitempty"`
	Baseline      int64     `json:"baseline,omitempty"`
	Latest        time.Time `json:"latest,omitempty"`
	LatestInput   int64     `json:"latestInput,omitempty"`
	Accepted      time.Time `json:"accepted,omitempty"`
	Pending       *Pending  `json:"pending,omitempty"`
}

func (s *State) Bind(id string, created, now time.Time) {
	if id == "" || now.IsZero() {
		return
	}
	if s.Binding != id {
		*s = State{Binding: id, Since: now}
	}
	if s.Since.IsZero() {
		s.Since = now
	}
	if !created.IsZero() && !created.After(now) {
		s.Created = created
	}
}

// Observe accepts only adapter-validated live ordinary requests. Repeated model
// steps contribute the minimum input for their turn, never extra turn votes.
func (s *State) Observe(turn, configuration string, input int64, at, now time.Time, counters ...int64) {
	if s.Binding == "" || turn == "" || configuration == "" || input <= 0 || at.IsZero() || at.After(now) || now.Sub(at) > WindowWidth || at.Before(s.Latest) {
		return
	}
	if configuration != s.Configuration {
		s.Configuration, s.Windows, s.Baseline = configuration, nil, 0
	}
	s.Windows = slices.DeleteFunc(s.Windows, func(w Window) bool { return now.Sub(w.Start) >= Retention })
	var counter int64
	if len(counters) > 0 {
		counter = counters[0]
	}
	for wi := range s.Windows {
		for si := range s.Windows[wi].Samples {
			v := &s.Windows[wi].Samples[si]
			if v.Turn == turn {
				if counter > 0 && counter <= v.Counter {
					return
				}
				v.Counter = counter
				v.Input = min(v.Input, input)
				s.Latest, s.LatestInput = at, v.Input
				return
			}
		}
	}
	start := at.UTC().Truncate(WindowWidth)
	if len(s.Windows) == 0 || !s.Windows[len(s.Windows)-1].Start.Equal(start) {
		s.Windows = append(s.Windows, Window{Start: start})
	}
	w := &s.Windows[len(s.Windows)-1]
	w.Samples = append(w.Samples, Sample{Turn: turn, At: at, Input: input, Counter: counter})
	if len(w.Samples) > TurnsPerWindow {
		w.Samples = w.Samples[len(w.Samples)-TurnsPerWindow:]
	}
	s.Latest, s.LatestInput = at, input
	if s.Baseline == 0 && sufficient(s.Windows) {
		s.Baseline = level(s.Windows)
	}
}

// ForgetTurn discards pre-compaction input from an in-progress turn. Prior
// ordinary turns remain request observations, never claimed compacted waterlines.
func (s *State) ForgetTurn(turn string) {
	for i := range s.Windows {
		s.Windows[i].Samples = slices.DeleteFunc(s.Windows[i].Samples, func(v Sample) bool { return v.Turn == turn })
	}
	s.Windows = slices.DeleteFunc(s.Windows, func(w Window) bool { return len(w.Samples) == 0 })
	s.Latest, s.LatestInput = time.Time{}, 0
}

func median(values []int64) int64 {
	slices.Sort(values)
	if len(values) == 0 {
		return 0
	}
	// Lower median requires more than half of an even sample to be high.
	return values[(len(values)-1)/2]
}
func windowLevel(w Window) int64 {
	values := make([]int64, 0, len(w.Samples))
	for _, v := range w.Samples {
		values = append(values, v.Input)
	}
	return median(values)
}
func level(windows []Window) int64 {
	values := make([]int64, 0, len(windows))
	for _, w := range windows {
		values = append(values, windowLevel(w))
	}
	return median(values)
}
func sufficient(windows []Window) bool {
	if len(windows) < MinWindows {
		return false
	}
	count := 0
	var first, last time.Time
	for _, w := range windows {
		for _, v := range w.Samples {
			count++
			if first.IsZero() || v.At.Before(first) {
				first = v.At
			}
			if v.At.After(last) {
				last = v.At
			}
		}
	}
	return count >= MinTurns && last.Sub(first) >= MinSpan
}
func (s *State) high(input int64) bool {
	return input >= HighInput || s.Baseline > 0 && input/2 >= s.Baseline && input-s.Baseline >= GrowthInput
}

func (s *State) Candidate(now time.Time) bool {
	birth := s.Created
	if birth.IsZero() {
		birth = s.Since
	}
	if birth.IsZero() || now.Sub(birth) < MinAge || s.Latest.IsZero() || s.Latest.After(now) || now.Sub(s.Latest) > WindowWidth || !s.high(s.LatestInput) {
		return false
	}
	cutoff := now.Add(-Retention)
	if !s.Accepted.IsZero() {
		if now.Before(s.Accepted.Add(Cooldown)) {
			return false
		}
		if s.Accepted.Add(Cooldown).After(cutoff) {
			cutoff = s.Accepted.Add(Cooldown)
		}
	}
	var eligible []Window
	for _, w := range s.Windows {
		fresh := Window{Start: w.Start}
		for _, v := range w.Samples {
			if v.At.After(cutoff) {
				fresh.Samples = append(fresh.Samples, v)
			}
		}
		if len(fresh.Samples) == 0 {
			continue
		}
		if !s.high(windowLevel(fresh)) {
			eligible = nil
			continue
		}
		eligible = append(eligible, fresh)
	}
	return sufficient(eligible)
}

func (s *State) Prepare(id string, now time.Time) string {
	if s.Pending != nil {
		if s.Pending.ID == id {
			return s.Pending.Text
		}
		return ""
	}
	if id == "" || !s.Candidate(now) {
		return ""
	}
	birth, qualifier := s.Created, "age"
	if birth.IsZero() {
		birth, qualifier = s.Since, "observed age lower bound"
	}
	text := fmt.Sprintf("[Private Bot continuity invitation; no new user request or authorization]\nSession %s: %d days. Ordinary-request input observations have stayed elevated across multiple days (recent turn minimum: %d tokens). These are request-input trends, not retained-context size, post-compaction waterlines, or billing cost. This is a one-time invitation for this submission. Prioritize the current user instruction and any correction or revocation. If context remains useful, continue normal work and defer review. Otherwise review continuity in small steps: refresh MEMORY.md in the Bot Notebook, use INDEX.md only to locate relevant recent notes, and retain current goals, preferences, authorization boundaries, pending interactions and original operation references. Use bot_dream only if you judge the handoff ready. This invitation requires no user-facing maintenance announcement.", qualifier, int(now.Sub(birth)/(24*time.Hour)), s.LatestInput)
	s.Pending = &Pending{ID: id, Text: text}
	return text
}

func (s *State) Resolve(id, outcome string, now time.Time) {
	if s.Pending == nil || s.Pending.ID != id {
		return
	}
	switch outcome {
	case "accepted":
		s.Accepted, s.Pending = now, nil
	case "rejected":
		s.Pending = nil
	}
}
