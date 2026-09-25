// Package care evaluates local, bounded conditions and queues ordinary Bot
// activations. Rules have no filesystem, network or process capabilities.
package care

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
)

const MaxRules = 32
const MaxEventBytes = 16384

var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type Rule struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	On              string `json:"on"`
	When            string `json:"when"`
	Prompt          string `json:"prompt"`
	TimeZone        string `json:"timeZone"`
	CooldownSeconds int    `json:"cooldownSeconds"`
	ExpiresSeconds  int    `json:"expiresSeconds"`
}
type Event struct {
	Source string         `json:"source"`
	At     time.Time      `json:"at"`
	Data   map[string]any `json:"data"`
}
type condition struct {
	program cel.Program
	zone    *time.Location
}

func compile(r Rule) (Rule, condition, error) {
	var c condition
	if !identifier.MatchString(r.ID) || strings.TrimSpace(r.Label) == "" || len(r.Label) > 200 || strings.TrimSpace(r.Prompt) == "" || len(r.Prompt) > 4096 {
		return r, c, errors.New("invalid rule id, label or prompt")
	}
	if !sourceName.MatchString(r.On) || len(r.On) > 128 {
		return r, c, errors.New("unsupported event source")
	}
	if len(r.When) > 2048 {
		return r, c, errors.New("condition exceeds 2 KiB")
	}
	if r.CooldownSeconds == 0 {
		r.CooldownSeconds = 3600
	}
	if r.ExpiresSeconds == 0 {
		r.ExpiresSeconds = 3600
	}
	if r.CooldownSeconds < 60 || r.CooldownSeconds > 366*86400 || r.ExpiresSeconds < 60 || r.ExpiresSeconds > 86400 {
		return r, c, errors.New("cooldown must be 60–31622400 seconds; expiry must be 60–86400 seconds")
	}
	var err error
	c.zone, err = time.LoadLocation(r.TimeZone)
	if err != nil || r.TimeZone == "" || r.TimeZone == "Local" {
		return r, c, errors.New("an explicit IANA timeZone is required")
	}
	env, err := cel.NewEnv(cel.Variable("event", cel.MapType(cel.StringType, cel.DynType)), cel.Variable("now", cel.TimestampType), cel.Variable("local", cel.MapType(cel.StringType, cel.IntType)), cel.ParserExpressionSizeLimit(2048), cel.ParserRecursionLimit(32))
	if err != nil {
		return r, c, err
	}
	ast, issues := env.Compile(r.When)
	if issues.Err() != nil {
		return r, c, fmt.Errorf("invalid CEL condition: %w", issues.Err())
	}
	if !ast.OutputType().IsExactType(cel.BoolType) {
		return r, c, errors.New("condition must return boolean")
	}
	c.program, err = env.Program(ast, cel.CostLimit(1000), cel.InterruptCheckFrequency(16))
	return r, c, err
}
func eventData(e Event) (map[string]any, error) {
	raw, err := json.Marshal(e.Data)
	if err != nil || len(raw) > MaxEventBytes {
		return nil, errors.New("event data exceeds 16 KiB or is not JSON")
	}
	// Normalize JSON integer numbers for CEL, including bridge-decoded float64s.
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var data map[string]any
	if err = dec.Decode(&data); err != nil || data == nil {
		return nil, errors.New("event data must be an object")
	}
	var normalize func(any) (any, error)
	normalize = func(v any) (any, error) {
		switch x := v.(type) {
		case json.Number:
			if n, err := x.Int64(); err == nil {
				return n, nil
			}
			n, err := x.Float64()
			if err != nil {
				return nil, errors.New("event number is out of range")
			}
			return n, nil
		case map[string]any:
			for k, v := range x {
				value, err := normalize(v)
				if err != nil {
					return nil, err
				}
				x[k] = value
			}
		case []any:
			for i, v := range x {
				value, err := normalize(v)
				if err != nil {
					return nil, err
				}
				x[i] = value
			}
		}
		return v, nil
	}
	value, err := normalize(data)
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}
func (c condition) evaluate(ctx context.Context, data map[string]any, now time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	t := now.In(c.zone)
	day := int(t.Weekday())
	if day == 0 {
		day = 7
	}
	v, _, err := c.program.ContextEval(ctx, map[string]any{"event": data, "now": now, "local": map[string]int{"year": t.Year(), "month": int(t.Month()), "day": t.Day(), "weekday": day, "hour": t.Hour(), "minute": t.Minute()}})
	if err != nil {
		return false, errors.New("condition evaluation failed (missing field, type or cost limit)")
	}
	return v == types.True, nil
}

// Test validates and evaluates a sample without registration, persistence or effects.
func Test(ctx context.Context, r Rule, data map[string]any, now time.Time) (bool, error) {
	if data == nil {
		data = map[string]any{}
	}
	_, c, err := compile(r)
	if err != nil {
		return false, err
	}
	data, err = eventData(Event{Data: data})
	if err != nil {
		return false, err
	}
	return c.evaluate(ctx, data, now)
}
