package main

// This POC has one effect: enqueue a prompt for an existing Bot. CEL cannot
// access the filesystem, network, processes, credentials, or presence policy.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
)

type Rule struct {
	ID              string `json:"id"`
	On              string `json:"on"`
	When            string `json:"when"`
	Prompt          string `json:"prompt"`
	CooldownSeconds int    `json:"cooldownSeconds"`
}
type Event struct {
	ID     string         `json:"id"`
	Source string         `json:"source"`
	Data   map[string]any `json:"data"`
}

// Presence is supplied by the platform adapter, never by a CEL rule/event body.
// Unknown lock state is deliberately distinct from unlocked.
type Presence struct {
	Awake    bool
	Unlocked *bool
}
type Activation struct {
	ID      string    `json:"id"`
	Rule    string    `json:"rule"`
	Version string    `json:"version"`
	Prompt  string    `json:"prompt"`
	Expires time.Time `json:"expires"`
	Status  string    `json:"status"`
}
type ledger struct {
	Version int                  `json:"version"`
	Entries []Activation         `json:"entries"`
	Last    map[string]time.Time `json:"last"`
}
type compiledRule struct {
	Rule
	version string
	program cel.Program
}
type engine struct {
	file  string
	rules []compiledRule
	state ledger
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:16])
}
func newEngine(file string, rules []Rule) (*engine, error) {
	if len(rules) > 32 {
		return nil, errors.New("at most 32 rules")
	}
	env, err := cel.NewEnv(cel.Variable("event", cel.MapType(cel.StringType, cel.DynType)), cel.Variable("now", cel.TimestampType), cel.ParserExpressionSizeLimit(2048), cel.ParserRecursionLimit(32))
	if err != nil {
		return nil, err
	}
	e := &engine{file: file, state: ledger{Version: 1, Last: map[string]time.Time{}}}
	ids := map[string]bool{}
	for _, r := range rules {
		if r.ID == "" || len(r.ID) > 128 || ids[r.ID] || r.On == "" || len(r.On) > 128 || r.Prompt == "" || len(r.Prompt) > 4096 || r.CooldownSeconds < 0 || r.CooldownSeconds > 86400*365 {
			return nil, errors.New("invalid rule")
		}
		ids[r.ID] = true
		ast, issues := env.Compile(r.When)
		if issues.Err() != nil {
			return nil, fmt.Errorf("rule %s: %w", r.ID, issues.Err())
		}
		if !ast.OutputType().IsExactType(cel.BoolType) {
			return nil, errors.New("condition must return bool")
		}
		program, err := env.Program(ast, cel.CostLimit(1000), cel.InterruptCheckFrequency(32))
		if err != nil {
			return nil, err
		}
		e.rules = append(e.rules, compiledRule{r, digest(r), program})
	}
	b, err := os.ReadFile(file)
	if err == nil {
		if len(b) > 2<<20 {
			return nil, errors.New("ledger too large")
		}
		if err = json.Unmarshal(b, &e.state); err != nil {
			return nil, err
		}
		if e.state.Version != 1 || e.state.Last == nil || len(e.state.Entries) > 256 {
			return nil, errors.New("invalid ledger")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return e, nil
}
func (e *engine) save() error {
	b, err := json.MarshalIndent(e.state, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(e.file), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(e.file), ".activation-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), e.file)
}

// Calls are serialized by the host event loop. Admission does not wake a model.
func (e *engine) receive(ctx context.Context, event Event, now time.Time) (int, error) {
	b, err := json.Marshal(event)
	if err != nil {
		return 0, err
	}
	if len(b) > 16384 || event.ID == "" || len(event.ID) > 128 || event.Source == "" {
		return 0, errors.New("invalid or oversized event")
	}
	var added []Activation
	for _, r := range e.rules {
		if r.On != event.Source {
			continue
		}
		id := digest([]string{r.version, event.Source, event.ID})
		seen := false
		for _, a := range e.state.Entries {
			if a.ID == id {
				seen = true
				break
			}
		}
		if seen {
			continue
		}
		if last := e.state.Last[r.version]; !last.IsZero() && now.Before(last.Add(time.Duration(r.CooldownSeconds)*time.Second)) {
			continue
		}
		value, _, err := r.program.ContextEval(ctx, map[string]any{"event": event.Data, "now": now})
		if err != nil {
			return 0, fmt.Errorf("rule %s evaluation: %w", r.ID, err)
		}
		if value != types.True {
			continue
		}
		added = append(added, Activation{ID: id, Rule: r.ID, Version: r.version, Prompt: r.Prompt, Expires: now.Add(time.Hour), Status: "pending"})
	}
	// Never discard idempotency receipts silently. This bounded POC requires a
	// fresh isolated ledger after 256 activations; production retention is separate.
	if len(e.state.Entries)+len(added) > 256 {
		return 0, errors.New("POC ledger capacity reached")
	}
	previous := e.state
	e.state.Entries = append(append([]Activation(nil), previous.Entries...), added...)
	e.state.Last = map[string]time.Time{}
	for k, v := range previous.Last {
		e.state.Last[k] = v
	}
	for _, a := range added {
		e.state.Last[a.Version] = now
	}
	if len(added) > 0 {
		if err = e.save(); err != nil {
			e.state = previous
			return 0, err
		}
	}
	return len(added), nil
}

// A callback error leaves dispatching durable. Restart never blindly repeats an
// uncertain effect. Runtime receipts must reconcile it before any later retry.
func (e *engine) deliver(now time.Time, p Presence, send func(Activation) error) (int, error) {
	if !p.Awake || p.Unlocked == nil || !*p.Unlocked {
		return 0, nil
	}
	count := 0
	for i := range e.state.Entries {
		a := &e.state.Entries[i]
		if a.Status != "pending" {
			continue
		}
		current := false
		for _, r := range e.rules {
			if r.ID == a.Rule && r.version == a.Version {
				current = true
				break
			}
		}
		if !current || !now.Before(a.Expires) {
			a.Status = "expired"
			if err := e.save(); err != nil {
				return count, err
			}
			continue
		}
		a.Status = "dispatching"
		if err := e.save(); err != nil {
			a.Status = "pending"
			return count, err
		}
		if err := send(*a); err != nil {
			return count, err
		}
		a.Status = "delivered"
		if err := e.save(); err != nil {
			a.Status = "dispatching"
			return count, err
		}
		count++
	}
	return count, nil
}
