package bot

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
	"github.com/caelis-labs/caelis-bot/internal/scheduledata"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
)

func (r *Runtime) Definitions() []api.ToolDefinition {
	out := toolDefinitions()
	if p, ok := r.engine.(api.ApplicationCapabilityProvider); ok {
		caps := p.ApplicationCapabilities()
		out = slices.DeleteFunc(out, func(d api.ToolDefinition) bool {
			return !caps.WorkerExecution && (d.Name == "bot_tasks" || d.Name == "bot_delegate" || d.Name == "bot_interactions") || !caps.ScheduledActivation && (d.Name == "bot_schedule_update" || d.Name == "bot_event")
		})
		if !caps.ScheduledActivation {
			for i, d := range out {
				if d.Name == "bot_schedule" {
					d.InputSchema, _ = json.Marshal(tc.Request(tc.Branch("context", tc.Schema{})))
					out[i] = d
				}
			}
		}
	}
	return out
}
func (r *Runtime) CallTool(ctx context.Context, name string, raw json.RawMessage) api.ToolResult {
	if name == "bot_care" || name == "bot_care_read" {
		return compactError("retired", errors.New("This tool is retired; use schedule. No effect was performed."))
	}
	r.toolAdmission.Lock()
	defer r.toolAdmission.Unlock()
	if err := ctx.Err(); err != nil {
		return compactError("cancelled", err)
	}
	r.mu.Lock()
	stopped := r.stopped
	r.mu.Unlock()
	if stopped {
		return compactError("stopped", errors.New("Bot stopped"))
	}
	if name != "bot_dream" && r.handoff != nil {
		if pending := r.handoff.pending(); pending.CallID != "" {
			return compactError("context_handoff", errors.New("Bot context handoff is completing; retry after the new session is ready"))
		}
	}
	var def *api.ToolDefinition
	for _, d := range r.Definitions() {
		if d.Name == name {
			def = &d
			break
		}
	}
	if def == nil {
		return compactError("unavailable", errors.New("tool unavailable in this runtime"))
	}
	args, err := tc.Decode(def.InputSchema, raw)
	if err != nil {
		return compactError("invalid_arguments", err)
	}
	if name == "bot_dream" {
		return r.callDreamTool(ctx, args)
	}
	if name == "bot_gesture" {
		return compact(r.callLegacyTool(ctx, name, raw), nil)
	}
	if name == "bot_delegate" {
		return r.callDelegate(ctx, args)
	}
	req := args["request"].(map[string]any)
	switch name {
	case "bot_event":
		return r.callEvent(ctx, req)
	case "bot_tasks":
		return r.callTasksV3(ctx, req)
	case "bot_interactions":
		return r.callCompactInteractions(ctx, req)
	case "bot_schedule", "bot_schedule_update":
		return r.callCompactSchedule(ctx, name, req)
	}
	return compactError("unavailable", errors.New("unsupported native tool"))
}
func compactError(code string, err error) api.ToolResult {
	v := map[string]any{"ok": false, "error": map[string]string{"code": code, "message": err.Error()}}
	if code == "invalid_arguments" {
		v["outcome"] = "rejected"
	}
	return compactValue(v, true)
}
func compactValue(value map[string]any, failed bool) api.ToolResult {
	b, _ := json.Marshal(value)
	if len(b) > 31<<10 {
		retained := map[string]any{}
		var data map[string]any
		_ = json.Unmarshal(encoded(value["data"]), &data)
		if data != nil {
			for _, key := range []string{"id", "requestId", "status", "outcome", "enabled"} {
				if v, exists := data[key]; exists {
					retained[key] = v
				}
			}
		}
		original := value
		value = map[string]any{"ok": false, "nativeOK": original["ok"], "data": retained, "error": map[string]string{"code": "model_output_budget", "message": "Native output exceeded the model budget. Narrow reads; retain the original mutation receipt and never replay for output."}}
		for _, key := range []string{"outcome", "next", "automation", "clock"} {
			if v, ok := original[key]; ok {
				value[key] = v
			}
		}
		b, _ = json.Marshal(value)
		failed = true
	}
	text := string(b)
	// MCP transports require content even with structuredContent. Large replies
	// use a short fallback instead of spending the budget on duplicate state.
	if len(b) > 8<<10 {
		text = "Read structuredContent for the typed native result."
	}
	return api.ToolResult{IsError: failed, StructuredContent: value, Content: []map[string]string{{"type": "text", "text": text}}}
}
func compact(out api.ToolResult, next map[string]any) api.ToolResult {
	var data any
	if out.StructuredContent != nil {
		data = out.StructuredContent
	} else if len(out.Content) > 0 {
		if json.Unmarshal([]byte(out.Content[0]["text"]), &data) != nil {
			data = out.Content[0]["text"]
		}
	}
	value := map[string]any{"ok": !out.IsError, "data": data}
	if next != nil {
		value["next"] = next
	}
	if obj, ok := data.(map[string]any); ok {
		if outcome, ok := obj["outcome"]; ok {
			value["outcome"] = outcome
		} else if task, ok := obj["task"].(map[string]any); ok {
			if outcome, ok := task["outcome"]; ok {
				value["outcome"] = outcome
			}
		}
	}
	if out.IsError {
		value["error"] = map[string]any{"code": "native_error", "message": "Native operation failed or remains unconfirmed; inspect the retained data."}
	}
	return compactValue(value, out.IsError)
}
func encoded(v any) json.RawMessage           { b, _ := json.Marshal(v); return b }
func str(m map[string]any, key string) string { s, _ := m[key].(string); return s }
func (r *Runtime) callCompactSchedule(ctx context.Context, name string, q map[string]any) api.ToolResult {
	if name == "bot_schedule" && str(q, "type") == "read" && strings.HasPrefix(str(q, "handle"), "calendar:") {
		id := strings.TrimPrefix(str(q, "handle"), "calendar:")
		for _, s := range r.State().Schedules {
			if s.Runtime == r.provider && s.ID == id {
				return boundedData(map[string]any{"ok": true, "data": s}, q)
			}
		}
		return compactError("unavailable", errors.New("original calendar arrangement unavailable"))
	}
	if out, handled := r.eventSchedule(ctx, name, q); handled {
		return out
	}
	kind := str(q, "type")
	if name == "bot_schedule" {
		switch kind {
		case "context":
			return compactValue(map[string]any{"ok": true, "data": r.Clock()}, false)
		case "list":
			return r.schedulePage(q)
		case "sources":
			data := map[string]any{"sources": scheduledata.NativeSources(), "eventTools": r.eventBindings(), "nativeAvailable": r.nativeErr == nil, "desktopObservationsAvailable": r.nativeSample != nil}
			if r.events != nil {
				data["policy"] = r.events.Snapshot().Policy
			}
			return boundedData(map[string]any{"ok": true, "data": data, "clock": r.Clock()}, q)
		case "test":
			trigger := q["trigger"].(map[string]any)
			matched, err := scheduledata.Test(ctx, str(trigger, "condition"), str(trigger, "timeZone"), q["event"].(map[string]any), r.now())
			if err != nil {
				return compactError("invalid_condition", err)
			}
			return compactValue(map[string]any{"ok": true, "data": map[string]bool{"matches": matched}}, false)
		}
	}
	var out api.ToolResult
	switch kind {
	case "configure":
		return r.configureSchedulePolicy(ctx, q)
	case "remove":
		domain, id, ok := strings.Cut(str(q, "automation"), ":")
		if !ok || id == "" {
			return compactError("invalid_arguments", errors.New("use exact returned automation handle"))
		}
		legacy := "bot_reminders"
		if domain == "event" {
			if err := r.eventError(); err != nil {
				return compactError("events_unavailable", err)
			}
			p, ok := r.events.NativePlan(id)
			if !ok {
				return compactError("unavailable", errors.New("native arrangement unavailable"))
			}
			out, _ := r.eventSchedule(ctx, name, map[string]any{"type": "remove", "automation": "schedule:" + p.ID})
			return out
		} else if domain != "calendar" {
			return compactError("invalid_arguments", errors.New("unknown automation kind"))
		}
		out = r.callLegacyTool(ctx, legacy, encoded(map[string]any{"operation": "remove", "id": id}))
	case "save":
		trigger := q["trigger"].(map[string]any)
		args := map[string]any{"operation": "save", "id": q["id"], "label": q["label"], "prompt": q["prompt"]}
		if str(trigger, "type") == "event" {
			return r.saveNativeSchedule(ctx, q)
		} else {
			schedule := trigger["schedule"].(map[string]any)
			for k, v := range schedule {
				if k != "type" {
					args[k] = v
				}
			}
			for _, key := range []string{"timeZone", "weekdays", "windowStart", "windowEnd"} {
				if v, ok := trigger[key]; ok {
					args[key] = v
				}
			}
			out = r.callLegacyTool(ctx, "bot_reminders", encoded(args))
		}
	}
	res := compact(out, nil)
	res.StructuredContent["clock"] = r.Clock()
	if kind == "save" {
		res.StructuredContent["automation"] = str(q["trigger"].(map[string]any), "type") + ":" + str(q, "id")
	}
	return compactValue(res.StructuredContent, res.IsError)
}
func (r *Runtime) schedulePage(q map[string]any) api.ToolResult {
	kind, query := str(q, "kind"), strings.ToLower(str(q, "query"))
	items := []map[string]any{}
	for _, s := range r.State().Schedules {
		if s.Runtime != r.provider || kind == "event" {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(s.ID+" "+s.Label), query) {
			continue
		}
		s.Prompt = "" // Read the exact calendar handle for the saved full context.
		items = append(items, map[string]any{"automation": "calendar:" + s.ID, "kind": "calendar", "id": s.ID, "label": s.Label, "next": s.Next, "enabled": s.Enabled, "schedule": s})
	}
	if kind != "calendar" && r.events != nil {
		state := r.events.Snapshot()
		labels := make(map[string]string, len(state.Events))
		for _, event := range state.Events {
			labels[event.ID] = event.Label
		}
		for _, p := range state.Plans {
			label := labels[p.Event]
			if (kind == "event" && p.Kind != "native") || !p.Finished.IsZero() || query != "" && !strings.Contains(strings.ToLower(p.ID+" "+p.Event+" "+p.NativeID+" "+label), query) {
				continue
			}
			item := map[string]any{"automation": "schedule:" + p.ID, "kind": "registered", "id": p.ID, "label": label, "event": "event:" + p.Event, "next": p.Next, "status": p.Status}
			if p.NativeID != "" {
				item["nativeId"] = p.NativeID
			}
			items = append(items, item)
		}
	}
	slices.SortFunc(items, func(a, b map[string]any) int { return strings.Compare(str(a, "automation"), str(b, "automation")) })
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded(map[string]any{"kind": kind, "query": query, "items": items})))
	offset := 0
	limit := 20
	if n, ok := q["limit"].(float64); ok {
		limit = int(n)
	}
	if token := str(q, "cursor"); token != "" {
		var c struct {
			Fingerprint string
			Offset      int
		}
		b, e := base64.RawURLEncoding.DecodeString(token)
		if e != nil || json.Unmarshal(b, &c) != nil || c.Fingerprint != fingerprint || c.Offset < 0 || c.Offset > len(items) {
			return compactError("cursor_invalid", errors.New("arrangements or filters changed; refresh first page"))
		}
		offset = c.Offset
	}
	end := offset
	for end < len(items) && end < offset+limit {
		if end > offset && len(encoded(items[offset:end+1])) > 16<<10 {
			break
		}
		end++
	}
	data := map[string]any{"items": items[offset:end], "total": len(items), "clock": r.Clock()}
	if end < len(items) {
		data["nextCursor"] = base64.RawURLEncoding.EncodeToString(encoded(struct {
			Fingerprint string
			Offset      int
		}{fingerprint, end}))
	}
	if kind != "calendar" {
		data["nativeAvailable"] = r.nativeErr == nil
		if r.events != nil {
			data["policy"] = r.events.Snapshot().Policy
		}

	}
	return compactValue(map[string]any{"ok": true, "data": data}, false)
}

// Keep the legacy view private to the original versioned invocation binding.
type legacyTools struct{ runtime *Runtime }

func (v legacyTools) Definitions() []api.ToolDefinition { return archivedCatalog("legacy") }
func (v legacyTools) CallTool(ctx context.Context, name string, args json.RawMessage) api.ToolResult {
	return v.runtime.callLegacyTool(ctx, name, args)
}
func (r *Runtime) LegacyToolConnection() *api.ToolConnection {
	approved := botpolicy.RetiredCatalogApprovedTools()
	return &api.ToolConnection{Host: legacyTools{r}, ApprovedTools: approved}
}

// The pre-retirement schema and approval policy are retained only to match
// pending Caelis callback versions. Dispatch still uses the current allowlist:
// bot_memory cannot execute through this historical binding.
type historicalLegacyTools struct{ runtime *Runtime }

func (v historicalLegacyTools) Definitions() []api.ToolDefinition {
	return archivedCatalog("legacy-memory")
}
func (v historicalLegacyTools) CallTool(ctx context.Context, name string, args json.RawMessage) api.ToolResult {
	return v.runtime.callLegacyTool(ctx, name, args)
}
func (r *Runtime) HistoricalToolConnections() []*api.ToolConnection {
	return []*api.ToolConnection{
		{Host: historicalLegacyTools{r}, ApprovedTools: botpolicy.HistoricalLegacyApprovedTools()},
		r.previousCompactToolConnection(),
		{Host: priorLifecycleTools{r}, ApprovedTools: botpolicy.ApprovedTools()},
		{Host: priorFollowupTools{r}, ApprovedTools: botpolicy.ApprovedTools()},
		{Host: priorEventTools{r}, ApprovedTools: botpolicy.ApprovedTools()},
		{Host: priorPresenceTools{r}, ApprovedTools: botpolicy.ApprovedTools()},
		{Host: priorQuestionTools{r}, ApprovedTools: botpolicy.ApprovedTools()},
	}
}
