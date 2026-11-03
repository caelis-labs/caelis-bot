package caelis

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func walkDTWJSON(value any, visit func(map[string]any)) {
	switch v := value.(type) {
	case map[string]any:
		visit(v)
		for _, child := range v {
			walkDTWJSON(child, visit)
		}
	case []any:
		for _, child := range v {
			walkDTWJSON(child, visit)
		}
	case string:
		trimmed := strings.TrimSpace(v)
		// MCP data may be wrapped in Core's untrusted-content delimiters.
		// Parse the embedded JSON as fixture data, never as authority.
		if start := strings.IndexAny(trimmed, "{["); start >= 0 {
			var nested any
			if json.NewDecoder(strings.NewReader(trimmed[start:])).Decode(&nested) == nil {
				walkDTWJSON(nested, visit)
			}
		}
	}
}

// Core's private ToolSearch selector uses the same local synthetic provider.
// Select only exact names supplied in its scoped candidate catalog; the real
// Core still owns search bindings, schemas, policy and all MCP execution.

func nativeDTWModel(model *acceptanceModel) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, "fixture read failed", 400)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var body any
		_ = json.Unmarshal(raw, &body)
		if request, ok := body.(map[string]any); ok {
			model.mu.Lock()
			model.requests["DTW_ALL_REQUESTS"] = append(model.requests["DTW_ALL_REQUESTS"], request)
			model.mu.Unlock()
			if text, ok := request["text"].(map[string]any); ok {
				format, _ := text["format"].(map[string]any)
				schema, _ := format["schema"].(map[string]any)
				props, _ := schema["properties"].(map[string]any)
				if props["option_id"] != nil {
					model.mu.Lock()
					model.requests["DTW_GUARDIAN"] = append(model.requests["DTW_GUARDIAN"], request)
					model.mu.Unlock()
					review := newAcceptanceModel()
					review.set("DTW_GUARDIAN", modelStep{Reply: `{"option_id":"allow_session","rationale":"Synthetic read-only app scope decision"}`})
					review.serveCase(w, r, "DTW_GUARDIAN")
					return
				}
			}
		}
		var names []string
		walkDTWJSON(body, func(v map[string]any) {
			candidates, ok := v["candidates"].([]any)
			if !ok || v["need"] == nil {
				return
			}
			for _, candidate := range candidates {
				if c, ok := candidate.(map[string]any); ok {
					if name, ok := c["name"].(string); ok {
						names = append(names, name)
					}
				}
			}
		})
		if len(names) != 0 {
			reply, _ := json.Marshal(map[string]any{"tools": names})
			selector := newAcceptanceModel()
			key := ""
			if cases := casePattern.FindAllString(string(raw), -1); len(cases) != 0 {
				key = cases[len(cases)-1]
			}
			selector.set(key, modelStep{Reply: string(reply)})
			selector.serve(w, r)
			return
		}
		model.serve(w, r)
	})
}

func discoveredDTWTool(t *testing.T, model *acceptanceModel, key, remote string) string {
	t.Helper()
	names := map[string]bool{}
	requests := model.seen(key)
	for i := 1; i < len(requests); i++ {
		walkDTWJSON(dtwLastToolOutput(requests[:i+1]), func(v map[string]any) {
			name, _ := v["name"].(string)
			if strings.HasSuffix(name, "__"+remote) && v["input_schema"] != nil && v["binding"] != nil {
				names[name] = true
			}
		})
	}
	if len(names) != 1 {
		t.Errorf("expected one source-bound discovered %s tool, found %d", remote, len(names))
		return ""
	}
	for name := range names {
		return name
	}
	return ""
}

func dtwExactText(value any, text string) bool {
	if v, ok := value.(string); ok && v == text {
		return true
	}
	found := false
	walkDTWJSON(value, func(v map[string]any) {
		for _, field := range v {
			if s, ok := field.(string); ok && s == text {
				found = true
			}
		}
	})
	return found
}

func dtwCompletedCall(model *acceptanceModel, key, marker string) string {
	call := ""
	for _, request := range model.seen(key) {
		walkDTWJSON(request, func(v map[string]any) {
			id, _ := v["call_id"].(string)
			if v["type"] == "function_call_output" && id != "" && dtwExactText(v["output"], marker) {
				call = id
			}
		})
	}
	return call
}

func dtwLastToolOutput(requests []map[string]any) any {
	if len(requests) == 0 {
		return nil
	}
	last := requests[len(requests)-1]
	var output any
	for _, field := range []string{"input", "messages"} {
		items, _ := last[field].([]any)
		for _, item := range items {
			message, _ := item.(map[string]any)
			if message["type"] == "function_call_output" {
				output = message["output"]
			} else if message["role"] == "tool" {
				output = message["content"]
			}
		}
	}
	return output
}

func approveDTWApp(t *testing.T, ctx context.Context, s *Session, app, source string) string {
	t.Helper()
	waitAcceptance(t, ctx, func() bool { return len(s.Snapshot().Approvals) != 0 || s.Snapshot().Phase == "completed" })
	approvals := s.Snapshot().Approvals
	if len(approvals) != 1 {
		t.Fatal("native app call did not request independent consent", len(approvals))
	}
	pending := approvals[0]
	s.mu.Lock()
	ref, ok := s.approvalLocked(pending.ID)
	native := clone(ref.active)
	s.mu.Unlock()
	if !ok || native == nil {
		t.Fatal("native approval target absent")
	}
	raw, _ := json.Marshal(native.Permission)
	var permission nativePermission
	if json.Unmarshal(raw, &permission) != nil {
		t.Fatal("native permission shape")
	}
	var input map[string]any
	if json.Unmarshal(permission.ToolCall.RawInput, &input) != nil || len(input) != 2 || input["app"] != app || input["source"] != source {
		t.Fatal("native app approval did not project actual exec arguments")
	}
	choice := ""
	for _, option := range pending.Choices {
		if option.ID == "allow_session" {
			choice = option.ID
		}
	}
	if choice == "" {
		t.Fatal("native scoped session consent unavailable")
	}
	if err := s.Decide(ctx, api.Decision{ID: pending.ID, Choice: choice}); err != nil {
		t.Fatal(err)
	}
	return pending.NativeID
}
