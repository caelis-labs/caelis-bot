// Package statemigration owns the one-time Bot binding v1 to v2 conversion.
// It is called only while loading durable state; dispatch never calls it.
// Remove the v1 importer when the minimum supported Bot state becomes v2.
// Retained receiptID fields remain ordinary observation addresses thereafter.
package statemigration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// Upgrade preserves original request/callback IDs and unknown effects. Only
// their receipt observation address changes to Core Store v3's namespace.
func Upgrade(raw []byte) ([]byte, bool, error) {
	var state map[string]json.RawMessage
	if err := json.Unmarshal(raw, &state); err != nil || state == nil {
		return nil, false, errors.New("invalid Bot state")
	}
	var version int
	if err := json.Unmarshal(state["version"], &version); err != nil {
		return nil, false, err
	}
	if version == 2 {
		return raw, false, nil
	}
	if version != 1 {
		return nil, false, errors.New("unsupported Bot state version")
	}
	var owner string
	if err := json.Unmarshal(state["principalID"], &owner); err != nil && state["principalID"] != nil {
		return nil, false, err
	}
	var scope struct {
		Principal   string `json:"principal_id"`
		Application string `json:"application_id"`
		Connection  string `json:"connection_id"`
	}
	if b := state["connection"]; b != nil {
		if err := json.Unmarshal(b, &scope); err != nil {
			return nil, false, err
		}
	}
	if scope.Principal != "" && scope.Principal != owner {
		return nil, false, errors.New("retired Bot owner mismatch")
	}
	for _, key := range []string{"operations", "typed"} {
		var records map[string]map[string]json.RawMessage
		if b := state[key]; b != nil {
			if err := json.Unmarshal(b, &records); err != nil {
				return nil, false, err
			}
		}
		for id, record := range records {
			if owner == "" || scope.Application == "" || scope.Connection == "" || id == "" || record == nil {
				return nil, false, errors.New("retired Bot receipt scope missing")
			}
			if record["receiptID"] != nil {
				return nil, false, errors.New("v1 state already contains receipt mapping")
			}
			var path string
			if err := json.Unmarshal(record["path"], &path); err != nil {
				return nil, false, err
			}
			parts := []string{owner, scope.Application, scope.Connection, id}
			prefix := "retired-app-op-"
			if key == "typed" && strings.HasSuffix(path, "/resources") {
				middle := strings.TrimSuffix(strings.TrimPrefix(path, "/application/sessions/"), "/resources")
				sid, err := url.PathUnescape(middle)
				if err != nil || sid == "" || !strings.HasPrefix(path, "/application/sessions/") || strings.Contains(middle, "/") {
					return nil, false, errors.New("retired resource Session missing")
				}
				parts = []string{owner, scope.Application, scope.Connection, sid, id}
				prefix = "retired-app-resource-op-"
			}
			b, _ := json.Marshal(parts)
			sum := sha256.Sum256(b)
			record["receiptID"], _ = json.Marshal(prefix + hex.EncodeToString(sum[:]))
			// Original intent bytes/digest and source operation remain unchanged. The
			// new path is for local routing/classification only, never redispatch.
			path = strings.Replace(path, "/application/sessions", "/sessions", 1)
			if path == "/application/workers" {
				path = "/sessions"
			}
			record["path"], _ = json.Marshal(path)
		}
		if records != nil {
			state[key], _ = json.Marshal(records)
		}
	}
	delete(state, "connection")
	state["version"] = json.RawMessage("2")
	out, err := json.Marshal(state)
	return out, true, err
}
