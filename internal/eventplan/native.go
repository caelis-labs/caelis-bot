package eventplan

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/scheduledata"
)

// NativeArrangement is the schedule API for host-owned observations. It does
// not own a queue or delivery loop: all observations enter Engine.Occurrences.
type NativeArrangement struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Prompt          string   `json:"prompt"`
	Sources         []string `json:"sources"`
	When            string   `json:"when"`
	TimeZone        string   `json:"timeZone"`
	CooldownSeconds int      `json:"cooldownSeconds"`
	ExpiresSeconds  int      `json:"expiresSeconds"`
	OriginRequests  []string `json:"-"`
}

func nativeID(id string) string {
	h := sha256.Sum256([]byte(id))
	return fmt.Sprintf("native-%x", h[:28])
}
func (p Plan) Authorization() string {
	if p.GrantID != "" {
		return p.GrantID
	}
	return "schedule:" + p.ID
}
func (e *Engine) NativePlan(id string) (Plan, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range e.state.Plans {
		if p.NativeID == id {
			return clone(p), true
		}
	}
	return Plan{}, false
}
func (e *Engine) HasNativePlans() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.ContainsFunc(e.state.Plans, func(p Plan) bool { return p.Kind == "native" && p.Status == "active" })
}
func normalizeNative(n NativeArrangement) (NativeArrangement, error) {
	if !identifier.MatchString(n.ID) || strings.TrimSpace(n.Label) == "" || len(n.Label) > 200 || strings.TrimSpace(n.Prompt) == "" || len(n.Prompt) > 4096 {
		return n, errors.New("invalid native arrangement")
	}
	if len(n.Sources) == 0 || len(n.Sources) > 32 {
		return n, errors.New("select registered native sources")
	}
	for _, s := range n.Sources {
		if !scheduledata.KnownSource(s) {
			return n, errors.New("unsupported native source")
		}
	}
	n.Sources = slices.Clone(n.Sources)
	slices.Sort(n.Sources)
	n.Sources = slices.Compact(n.Sources)
	if n.CooldownSeconds == 0 {
		n.CooldownSeconds = 3600
	}
	if n.ExpiresSeconds == 0 {
		n.ExpiresSeconds = 3600
	}
	if n.CooldownSeconds < 60 || n.CooldownSeconds > 31622400 || n.ExpiresSeconds < 60 || n.ExpiresSeconds > 86400 {
		return n, errors.New("invalid cooldown or pending lifetime")
	}
	return n, scheduledata.Validate(n.When, n.TimeZone)
}
func (e *Engine) SaveNative(n NativeArrangement, now time.Time, authorize func(Plan) error) (Plan, error) {
	n, err := normalizeNative(n)
	if err != nil {
		return Plan{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err = e.usable(); err != nil {
		return Plan{}, err
	}
	id := nativeID(n.ID)
	p := Plan{ID: id, NativeID: n.ID, Event: id, Kind: "native", Sources: n.Sources, PendingSeconds: n.ExpiresSeconds, When: n.When, TimeZone: n.TimeZone, CooldownSeconds: n.CooldownSeconds, OriginRequests: n.OriginRequests, Created: now, Next: now, Expires: time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), Status: "active"}
	v := Event{ID: id, Label: n.Label, Context: n.Prompt, Kind: "native", OutputSchema: json.RawMessage(`{"type":"object"}`), Created: now, LastUsed: now}
	for _, old := range e.state.Plans {
		if old.NativeID == n.ID || old.ID == id {
			event, _ := e.event(old.Event)
			check := p
			check.GrantID = old.GrantID
			check.LegacyVersion = old.LegacyVersion
			if reflect.DeepEqual(definition(old), definition(check)) && event.Label == v.Label && event.Context == v.Context {
				return clone(old), nil
			}
			return Plan{}, errors.New("arrangement id already bound; remove it and use a new id")
		}
	}
	// Plan and Event retention clocks differ. Even after the Plan is pruned,
	// its retained (possibly archived) definition still owns this identity.
	if slices.ContainsFunc(e.state.Events, func(old Event) bool { return old.ID == id }) {
		return Plan{}, errors.New("arrangement id still retained; use a new id")
	}
	if err := e.planCapacity(); err != nil {
		return Plan{}, err
	}
	if err := e.eventCapacity(); err != nil {
		return Plan{}, err
	}
	if authorize != nil {
		if err = authorize(p); err != nil {
			return Plan{}, err
		}
	}
	e.state.Events = append(e.state.Events, v)
	e.state.Plans = append(e.state.Plans, p)
	return p, e.persist()
}

// ReceiveNative only accepts the host's bounded sample stream. There is no MCP
// event-publishing API. Watermarks and pending work commit in the same write.
func (e *Engine) ReceiveNative(ctx context.Context, events []scheduledata.Event, now time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.usable(); err != nil {
		return err
	}
	before := clone(e.state)
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !scheduledata.KnownSource(event.Source) || event.At.IsZero() || event.At.After(now) || !event.At.After(e.state.NativeWatermarks[event.Source]) {
			continue
		}
		if e.state.NativeWatermarks == nil {
			e.state.NativeWatermarks = map[string]time.Time{}
		}
		e.state.NativeWatermarks[event.Source] = event.At
		for i := range e.state.Plans {
			p := &e.state.Plans[i]
			if p.Kind != "native" || p.Status != "active" || !now.Before(p.Expires) || !slices.Contains(p.Sources, event.Source) || e.pending(p.ID) || !p.LastWake.IsZero() && now.Before(p.LastWake.Add(time.Duration(p.CooldownSeconds)*time.Second)) {
				continue
			}
			raw, err := json.Marshal(event.Data)
			if err != nil || len(raw) > scheduledata.MaxEventBytes {
				continue
			}
			o := Occurrence{ID: "occurrence:" + rand.Text(), Plan: p.ID, Event: p.Event, At: now, Expires: now.Add(time.Duration(p.PendingSeconds) * time.Second), Status: "collecting"}
			e.state.Occurrences = append(e.state.Occurrences, o)
			e.finishCollection(completion{id: o.ID, output: raw}, now)
		}
	}
	if reflect.DeepEqual(before, e.state) {
		return nil
	}
	return e.persist()
}

type Policy struct {
	MaximumInterruptionsPer24Hours int `json:"maximumInterruptionsPer24Hours"`
	MinimumGapSeconds              int `json:"minimumGapSeconds"`
}

func DefaultPolicy() Policy { return Policy{8, 300} }
func (p Policy) valid() bool {
	return p.MaximumInterruptionsPer24Hours >= 1 && p.MaximumInterruptionsPer24Hours <= 256 && p.MinimumGapSeconds >= 0 && p.MinimumGapSeconds <= 86400
}
func (e *Engine) ConfigurePolicy(p Policy, authorize func() error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.usable(); err != nil {
		return err
	}
	if !p.valid() {
		return errors.New("invalid schedule budget")
	}
	if e.state.Policy == p {
		return nil
	}
	if authorize != nil {
		if err := authorize(); err != nil {
			return err
		}
	}
	e.state.Policy = p
	return e.persist()
}
