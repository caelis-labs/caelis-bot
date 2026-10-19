// Package scheduledata provides pure conditions and native observations, never dispatch policy.
package scheduledata

import (
	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const MaxEventBytes = 16384

type condition struct {
	program cel.Program
	zone    *time.Location
}

func compile(when, zone string) (condition, error) {
	var c condition
	if len(when) > 2048 {
		return c, errors.New("condition exceeds 2 KiB")
	}
	var err error
	c.zone, err = time.LoadLocation(zone)
	if err != nil || zone == "" || zone == "Local" {
		return c, errors.New("an explicit IANA timeZone is required")
	}
	env, err := cel.NewEnv(cel.Variable("event", cel.MapType(cel.StringType, cel.DynType)), cel.Variable("now", cel.TimestampType), cel.Variable("local", cel.MapType(cel.StringType, cel.IntType)), cel.ParserExpressionSizeLimit(2048), cel.ParserRecursionLimit(32))
	if err != nil {
		return c, err
	}
	ast, issues := env.Compile(when)
	if issues.Err() != nil {
		return c, fmt.Errorf("invalid CEL condition: %w", issues.Err())
	}
	if !ast.OutputType().IsExactType(cel.BoolType) {
		return c, errors.New("condition must return boolean")
	}
	c.program, err = env.Program(ast, cel.CostLimit(1000), cel.InterruptCheckFrequency(16))
	return c, err
}
func eventData(input map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(input)
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
func Test(ctx context.Context, when, zone string, data map[string]any, now time.Time) (bool, error) {
	if data == nil {
		data = map[string]any{}
	}
	c, err := compile(when, zone)
	if err != nil {
		return false, err
	}
	data, err = eventData(data)
	if err != nil {
		return false, err
	}
	return c.evaluate(ctx, data, now)
}

func Validate(when, zone string) error { _, err := compile(when, zone); return err }
