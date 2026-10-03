// Package toolcontract defines the bounded JSON grammar shared by Bot catalogs
// and their native dispatchers. It validates only application-owned schemas.
package toolcontract

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/desktop-world/protocol"
)

type Schema = map[string]any

func Object(properties Schema, required ...string) Schema {
	if required == nil {
		required = []string{}
	}
	return Schema{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func String(description string) Schema { return Schema{"type": "string", "description": description} }
func Enum(values ...string) Schema     { return Schema{"type": "string", "enum": values} }
func Integer(min, max int) Schema      { return Schema{"type": "integer", "minimum": min, "maximum": max} }
func Branch(kind string, fields Schema, required ...string) Schema {
	fields["type"] = Schema{"type": "string", "const": kind}
	return Object(fields, append([]string{"type"}, required...)...)
}
func Request(branches ...Schema) Schema {
	return Object(Schema{"request": Schema{"anyOf": branches}}, "request")
}
func Definition(name, description string, schema Schema) api.ToolDefinition {
	raw, _ := json.Marshal(schema)
	return api.ToolDefinition{Name: name, Description: description, InputSchema: raw, ResultFormat: "content-v1"}
}
func Decode(schema json.RawMessage, raw json.RawMessage) (Schema, error) {
	if len(raw) > 128<<10 {
		return nil, fmt.Errorf("arguments exceed 128 KiB")
	}
	var value Schema
	if err := protocol.Decode(raw, &value); err != nil || value == nil {
		return nil, fmt.Errorf("arguments must be one JSON object without duplicate keys")
	}
	var s Schema
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil, fmt.Errorf("invalid native tool schema")
	}
	if err := validate(s, value, "arguments"); err != nil {
		return nil, err
	}
	return value, nil
}
func array(v any) []any {
	if v == nil {
		return nil
	}
	r := reflect.ValueOf(v)
	if r.Kind() != reflect.Slice {
		return nil
	}
	out := make([]any, r.Len())
	for i := range out {
		out[i] = r.Index(i).Interface()
	}
	return out
}
func same(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func number(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}
func validate(s Schema, v any, path string) error {
	fail := func(message string) error { return fmt.Errorf("%s: %s", path, message) }
	for _, keyword := range []string{"anyOf", "oneOf"} {
		if branches := array(s[keyword]); branches != nil {
			// A typed request normally identifies one branch. Report its actual
			// error instead of dumping errors from every unrelated operation.
			if keyword == "anyOf" {
				if obj, ok := v.(map[string]any); ok {
					var matching []any
					for _, b := range branches {
						props, _ := b.(map[string]any)["properties"].(map[string]any)
						kind, _ := props["type"].(map[string]any)
						if c, exists := kind["const"]; exists && same(c, obj["type"]) {
							matching = append(matching, b)
						}
					}
					if len(matching) == 1 {
						branches = matching
					}
				}
			}
			hits := 0
			var reasons []string
			for _, b := range branches {
				err := validate(b.(map[string]any), v, path)
				if err == nil {
					hits++
				} else {
					reasons = append(reasons, err.Error())
				}
			}
			if hits == 0 || keyword == "oneOf" && hits != 1 {
				detail := strings.Join(reasons, "; ")
				if len(detail) > 2048 {
					detail = detail[:2048] + "..."
				}
				return fail("must match one documented request variant (" + detail + ")")
			}
		}
	}
	if n, ok := s["not"].(map[string]any); ok && validate(n, v, path) == nil {
		return fail("incompatible fields")
	}
	if c, ok := s["const"]; ok && !same(c, v) {
		return fail(fmt.Sprintf("expected %v", c))
	}
	if choices := array(s["enum"]); choices != nil {
		found := false
		for _, c := range choices {
			found = found || same(c, v)
		}
		if !found {
			return fail("unsupported value")
		}
	}
	switch s["type"] {
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return fail("must be an object")
		}
	case "array":
		if _, ok := v.([]any); !ok {
			return fail("must be an array")
		}
	case "string":
		if _, ok := v.(string); !ok {
			return fail("must be a string")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fail("must be a boolean")
		}
	case "integer", "number":
		n, ok := v.(float64)
		if !ok {
			return fail("must be a number")
		}
		if s["type"] == "integer" && n != float64(int64(n)) {
			return fail("must be an integer")
		}
	}
	if obj, ok := v.(map[string]any); ok {
		for _, key := range array(s["required"]) {
			if _, ok := obj[key.(string)]; !ok {
				return fail("missing " + key.(string))
			}
		}
		if max, ok := s["maxProperties"]; ok && len(obj) > int(number(max)) {
			return fail("too many fields")
		}
		props, _ := s["properties"].(map[string]any)
		for key, value := range obj {
			if p, ok := props[key].(map[string]any); ok {
				if err := validate(p, value, path+"."+key); err != nil {
					return err
				}
			} else if s["additionalProperties"] == false {
				return fail("unknown field " + key)
			} else if p, ok := s["additionalProperties"].(map[string]any); ok {
				if err := validate(p, value, path+"."+key); err != nil {
					return err
				}
			}
		}
	}
	if str, ok := v.(string); ok {
		for _, bound := range []struct {
			key string
			bad func(int, int) bool
		}{{"minLength", func(a, b int) bool { return a < b }}, {"maxLength", func(a, b int) bool { return a > b }}} {
			if n, ok := s[bound.key]; ok && bound.bad(len([]rune(str)), int(number(n))) {
				return fail("string length outside allowed range")
			}
		}
		if pattern, ok := s["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(str) {
			return fail("invalid string format")
		}
	}
	if n, ok := v.(float64); ok {
		if min, ok := s["minimum"]; ok && n < number(min) {
			return fail("below minimum")
		}
		if max, ok := s["maximum"]; ok && n > number(max) {
			return fail("above maximum")
		}
	}
	if items, ok := v.([]any); ok {
		if min, ok := s["minItems"]; ok && len(items) < int(number(min)) {
			return fail("too few items")
		}
		if max, ok := s["maxItems"]; ok && len(items) > int(number(max)) {
			return fail("too many items")
		}
		for i, value := range items {
			if p, ok := s["items"].(map[string]any); ok {
				if err := validate(p, value, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
			if s["uniqueItems"] == true {
				for j := 0; j < i; j++ {
					if same(value, items[j]) {
						return fail("duplicate items")
					}
				}
			}
		}
	}
	return nil
}
