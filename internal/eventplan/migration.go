package eventplan

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/scheduledata"
	"io"
	"os"
	"slices"
	"time"
)

// Migration is an atomic import checkpoint in the destination ledger. The old
// file is read-only and never reopened after this marker is durable.
type Migration struct {
	Version   int       `json:"version"`
	Digest    string    `json:"digest"`
	Completed time.Time `json:"completed"`
}

// These are storage decoders, not an executable legacy engine or API.
type legacyRule struct {
	ID, Label, On, When, Prompt, TimeZone, Version string
	OnAny, OriginRequests                          []string
	CooldownSeconds, ExpiresSeconds                int
	Enabled                                        bool
	Last                                           time.Time
	Issue                                          string
}
type legacyActivation struct {
	ID, RuleID, Version, Prompt, Status, Result, Source string
	OriginRequests                                      []string
	Expires, DispatchedAt, VisibleAt                    time.Time
}
type legacyState struct {
	Version                                 int
	Rules                                   []legacyRule
	Activations                             []legacyActivation
	Watermarks                              map[string]time.Time
	Attempts, Interruptions, LegacyAttempts []time.Time
	Policy                                  Policy
}

func (e *Engine) MigrateLegacy(path string, now time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.usable(); err != nil {
		return err
	}
	if e.state.Migration != nil {
		return nil
	}
	info, err := os.Lstat(path)
	if err == nil && (!info.Mode().IsRegular() || info.Size() > 2<<20) {
		return errors.New("invalid saved native arrangement file; original data preserved")
	}
	var raw []byte
	if err == nil {
		var file *os.File
		file, err = os.Open(path)
		if err == nil {
			raw, err = io.ReadAll(io.LimitReader(file, (2<<20)+1))
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		e.state.Migration = &Migration{Version: 1, Digest: "absent", Completed: now}
		return e.persist()
	}
	if err != nil {
		return errors.New("saved native arrangements unavailable; original data preserved")
	}
	var old legacyState
	if len(raw) > 2<<20 || json.Unmarshal(raw, &old) != nil || (old.Version != 1 && old.Version != 2) || old.Watermarks == nil || !old.Policy.valid() || len(old.Rules) > 32 || len(old.Activations) > 256 || len(old.Attempts) > 256 || len(old.Interruptions) > 256 || len(old.LegacyAttempts) > 8 {
		return errors.New("invalid saved native arrangements; original data preserved")
	}
	next := clone(e.state)
	byRule := map[string]Plan{}
	versions := map[string]string{}
	appendPlan := func(p Plan, v Event) error {
		if slices.ContainsFunc(next.Plans, func(other Plan) bool { return other.ID == p.ID }) || slices.ContainsFunc(next.Events, func(other Event) bool { return other.ID == v.ID }) {
			return errors.New("migration identity collision; original data preserved")
		}
		next.Plans = append(next.Plans, p)
		next.Events = append(next.Events, v)
		return nil
	}
	for _, r := range old.Rules {
		sources := r.OnAny
		if r.On != "" {
			if len(sources) > 0 {
				return errors.New("ambiguous saved sources")
			}
			sources = []string{r.On}
		}
		n, err := normalizeNative(NativeArrangement{ID: r.ID, Label: r.Label, Prompt: r.Prompt, Sources: sources, When: r.When, TimeZone: r.TimeZone, CooldownSeconds: r.CooldownSeconds, ExpiresSeconds: r.ExpiresSeconds})
		if err != nil || r.Version == "" || n.CooldownSeconds != r.CooldownSeconds || n.ExpiresSeconds != r.ExpiresSeconds {
			return errors.New("invalid saved native rule; original data preserved")
		}
		if _, ok := byRule[r.ID]; ok {
			return errors.New("duplicate saved native rule")
		}
		id := nativeID(r.ID)
		p := Plan{ID: id, NativeID: r.ID, Event: id, GrantID: "care:" + r.ID, LegacyVersion: r.Version, Kind: "native", Sources: n.Sources, PendingSeconds: n.ExpiresSeconds, When: r.When, TimeZone: r.TimeZone, CooldownSeconds: n.CooldownSeconds, OriginRequests: r.OriginRequests, Created: now, Next: now, Expires: time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), LastWake: r.Last, Status: "active", Issue: r.Issue}
		if !r.Enabled {
			p.Status = "cancelled"
			p.Finished = now
		}
		v := Event{ID: id, Label: r.Label, Context: r.Prompt, Kind: "native", OutputSchema: json.RawMessage(`{"type":"object"}`), Created: now, LastUsed: now}
		if err = appendPlan(p, v); err != nil {
			return err
		}
		byRule[r.ID] = p
		versions[r.ID] = r.Version
	}
	seen := map[string]bool{}
	for _, a := range old.Activations {
		if a.ID == "" || a.Version == "" || seen[a.ID] || !slices.Contains([]string{"pending", "dispatching", "unknown", "accepted", "rejected", "expired", "cancelled"}, a.Status) || !slices.Contains([]string{"", "visible", "silent", "legacy"}, a.Result) {
			return errors.New("invalid saved activation; original data preserved")
		}
		seen[a.ID] = true
		if slices.ContainsFunc(next.Occurrences, func(o Occurrence) bool { return o.ID == a.ID }) {
			return errors.New("migration occurrence collision")
		}
		p, ok := byRule[a.RuleID]
		if !ok || versions[a.RuleID] != a.Version {
			// Superseded/removed records have no executable arrangement. Retain the
			// original grant and ID solely for terminal receipt reconciliation.
			id := nativeID("receipt:" + a.ID)
			p = Plan{ID: id, Event: id, GrantID: "care:" + a.RuleID, LegacyVersion: a.Version, Kind: "native", PendingSeconds: 3600, When: "true", TimeZone: "UTC", CooldownSeconds: 3600, OriginRequests: a.OriginRequests, Created: now, Next: now, Expires: now, Status: "cancelled", Finished: now}
			if err := appendPlan(p, Event{ID: id, Label: "Previous arrangement", Context: a.Prompt, Kind: "native", OutputSchema: json.RawMessage(`{"type":"object"}`), Created: now, LastUsed: now}); err != nil {
				return err
			}
		}
		at := a.DispatchedAt
		if at.IsZero() {
			at = now
		}
		o := Occurrence{OriginRequests: a.OriginRequests, ID: a.ID, Plan: p.ID, Event: p.Event, At: at, Expires: a.Expires, Status: a.Status}
		switch a.Status {
		case "pending":
			if a.Expires.IsZero() {
				return errors.New("missing saved activation expiry")
			}
			if !p.Finished.IsZero() || a.Source != "" && !scheduledata.KnownSource(a.Source) {
				o.Status = "cancelled"
				o.Finished = now
			} else if !now.Before(a.Expires) {
				o.Status = "expired"
				o.Finished = now
			}
		case "unknown", "dispatching":
			o.Status = "unknown_wake"
		case "accepted":
			if a.Result != "" || old.Version == 1 {
				o.Status = "completed"
				o.Finished = now
			}
		case "rejected":
			o.Status = "failed"
			o.Finished = now
		default:
			o.Finished = now
		}
		next.Occurrences = append(next.Occurrences, o)
	}
	if next.NativeWatermarks == nil {
		next.NativeWatermarks = map[string]time.Time{}
	}
	for source, at := range old.Watermarks {
		if at.After(next.NativeWatermarks[source]) {
			next.NativeWatermarks[source] = at
		}
	}
	// Preserve the stricter limit when two previously separate budgets combine.
	next.Policy.MaximumInterruptionsPer24Hours = min(next.Policy.MaximumInterruptionsPer24Hours, old.Policy.MaximumInterruptionsPer24Hours)
	next.Policy.MinimumGapSeconds = max(next.Policy.MinimumGapSeconds, old.Policy.MinimumGapSeconds)
	// Preserve dispatch multiplicity even when a zero-gap policy produced
	// identical timestamps. Visibility/unknown reservations beyond that count
	// consume quota without inventing a new dispatch or moving the gap.
	recent := func(times []time.Time) []time.Time {
		return slices.DeleteFunc(slices.Clone(times), func(at time.Time) bool { return !at.After(now.Add(-24 * time.Hour)) })
	}
	attempts := recent(old.Attempts)
	next.WakeAttempts = append(next.WakeAttempts, attempts...)
	occupied := append(recent(old.Interruptions), recent(old.LegacyAttempts)...)
	for _, a := range old.Activations {
		if a.VisibleAt.IsZero() && (a.Status == "unknown" || a.Status == "dispatching" || old.Version == 2 && a.Status == "accepted" && a.Result == "") {
			occupied = append(occupied, now)
		}
	}
	slices.SortFunc(occupied, func(a, b time.Time) int { return a.Compare(b) })
	if extra := len(occupied) - len(attempts); extra > 0 {
		next.WakeReservations = append(next.WakeReservations, occupied[len(occupied)-extra:]...)
	}
	slices.SortFunc(next.WakeAttempts, func(a, b time.Time) int { return a.Compare(b) })
	if len(next.Plans) > 1024 || len(next.Events) > 1024 || len(next.Occurrences) > 1024 {
		return errors.New("migration exceeds bounded storage")
	}
	for _, v := range next.Events {
		if err := validateEvent(v); err != nil {
			return errors.New("invalid imported event; original data preserved")
		}
	}
	h := sha256.Sum256(raw)
	next.Migration = &Migration{Version: 1, Digest: fmt.Sprintf("%x", h), Completed: now}
	e.state = next
	return e.persist()
}
