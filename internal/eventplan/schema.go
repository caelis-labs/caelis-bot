// Package eventplan owns bounded collectors and durable Bot wakeups. It never
// runs a model to poll and never treats a collector result as authorization.
package eventplan

import (
	"encoding/json"
	"errors"
	"fmt"

	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
)

// ValidateSchema accepts a deliberately small, explicit JSON Schema subset.
// Rejecting unknown keywords avoids silently weakening a Bot-authored contract.
func ValidateSchema(raw json.RawMessage) error {
	if len(raw) > 8192 {
		return errors.New("outputSchema exceeds 8 KiB")
	}
	var s map[string]any
	if json.Unmarshal(raw, &s) != nil || s["type"] != "object" {
		return errors.New("outputSchema must describe an object")
	}
	return schemaNode(s, 0)
}
func schemaNode(s map[string]any, depth int) error {
	if depth > 8 {
		return errors.New("outputSchema nesting exceeds 8")
	}
	for k := range s {
		switch k {
		case "type", "properties", "required", "additionalProperties", "items", "description":
		default:
			return fmt.Errorf("unsupported outputSchema keyword %s", k)
		}
	}
	switch s["type"] {
	case "object", "array", "string", "boolean", "integer", "number":
	default:
		return errors.New("each outputSchema node needs one supported type")
	}
	if v, ok := s["description"]; ok {
		if _, ok := v.(string); !ok {
			return errors.New("description must be text")
		}
	}
	props := map[string]any{}
	if v, ok := s["properties"]; ok {
		var valid bool
		props, valid = v.(map[string]any)
		if !valid || s["type"] != "object" || len(props) > 64 {
			return errors.New("invalid properties")
		}
		for _, p := range props {
			n, ok := p.(map[string]any)
			if !ok {
				return errors.New("invalid property schema")
			}
			if err := schemaNode(n, depth+1); err != nil {
				return err
			}
		}
	}
	if v, ok := s["required"]; ok {
		keys, ok := v.([]any)
		if !ok || s["type"] != "object" {
			return errors.New("required must be an array")
		}
		for _, k := range keys {
			key, ok := k.(string)
			if !ok || props[key] == nil {
				return errors.New("required must name declared properties")
			}
		}
	}
	if v, ok := s["additionalProperties"]; ok {
		if _, ok := v.(bool); !ok || s["type"] != "object" {
			return errors.New("additionalProperties must be boolean")
		}
	}
	if s["type"] == "array" {
		n, ok := s["items"].(map[string]any)
		if !ok {
			return errors.New("array needs items")
		}
		return schemaNode(n, depth+1)
	}
	if _, ok := s["items"]; ok {
		return errors.New("items requires array")
	}
	return nil
}
func ValidateOutput(schema, raw json.RawMessage) (map[string]any, error) {
	if len(raw) > 16384 {
		return nil, errors.New("event output exceeds 16 KiB")
	}
	if err := ValidateSchema(schema); err != nil {
		return nil, err
	}
	return tc.Decode(schema, raw)
}
