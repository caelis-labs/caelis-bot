package bot

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
)

func (r *Runtime) Definitions() []api.ToolDefinition {
	out := toolDefinitions()
	if r.desktopControl != nil {
		out = append(out, desktopcontrol.Definitions()...)
	}
	if p, ok := r.engine.(api.ApplicationCapabilityProvider); ok {
		caps := p.ApplicationCapabilities()
		out = slices.DeleteFunc(out, func(d api.ToolDefinition) bool {
			return !caps.WorkerExecution && (d.Name == "bot_tasks" || d.Name == "bot_delegate") || !caps.ScheduledActivation && d.Name == "bot_schedule_update"
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
	if err := ctx.Err(); err != nil {
		return compactError("cancelled", err)
	}
	r.mu.Lock()
	stopped := r.stopped
	r.mu.Unlock()
	if stopped {
		return compactError("stopped", errors.New("Bot stopped"))
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
	if strings.HasPrefix(name, desktopcontrol.Prefix) {
		return r.callCompactDesktop(ctx, name, args)
	}
	if name == "bot_memory" || name == "bot_gesture" {
		return compact(r.callLegacyTool(ctx, name, raw), nil)
	}
	req := args["request"].(map[string]any)
	switch name {
	case "bot_tasks", "bot_delegate":
		return r.callCompactTask(ctx, name, req)
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
		if data, ok := value["data"].(map[string]any); ok {
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
func (r *Runtime) callCompactTask(ctx context.Context, name string, q map[string]any) api.ToolResult {
	kind := str(q, "type")
	delete(q, "type")
	if name == "bot_delegate" {
		legacy := "bot_task_start"
		if kind == "continue" {
			legacy = "bot_task_send"
			q["id"] = q["task"]
			delete(q, "task")
		}
		out := r.callLegacyTool(ctx, legacy, encoded(q))
		var value map[string]any
		if len(out.Content) > 0 {
			_ = json.Unmarshal([]byte(out.Content[0]["text"]), &value)
		}
		task := str(value, "id")
		if inner, ok := value["task"].(map[string]any); ok {
			task = str(inner, "id")
		}
		target := map[string]any{"type": "read", "requestId": q["requestId"]}
		if task != "" {
			target = map[string]any{"type": "read", "task": task}
		}
		when := "on_completion_or_user_request"
		if out.IsError {
			when = "now"
		}
		return compact(out, map[string]any{"tool": "bot_tasks", "request": target, "when": when})
	}
	switch kind {
	case "machines":
		r.mu.Lock()
		provider := r.tasks
		r.mu.Unlock()
		machines := []api.TaskMachine{}
		if p, ok := provider.(api.TaskMachineProvider); ok {
			for _, m := range p.TaskMachines() {
				if query := strings.ToLower(str(q, "query")); query == "" || strings.Contains(strings.ToLower(m.Name), query) {
					machines = append(machines, m)
				}
			}
		}
		return compactValue(map[string]any{"ok": true, "data": machines}, false)
	case "read":
		if id := str(q, "requestId"); id != "" {
			r.mu.Lock()
			p, ok := r.tasks.(api.TaskRequestReader)
			r.mu.Unlock()
			if !ok {
				return compactError("receipt_unavailable", errors.New("request lookup unavailable; no replay"))
			}
			bounded, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			task, e := p.ReadTaskRequest(bounded, id)
			return compact(result(task, e), nil)
		}
		return compact(r.callLegacyTool(ctx, "bot_task_read", encoded(map[string]any{"id": q["task"]})), nil)
	case "stop":
		return compact(r.callLegacyTool(ctx, "bot_task_stop", encoded(map[string]any{"id": q["task"]})), nil)
	case "list":
		q["operation"] = "list"
	case "watchlist":
		q["operation"] = q["action"]
		delete(q, "action")
		if id, ok := q["task"]; ok {
			q["id"] = id
			delete(q, "task")
		}
	}
	return compact(r.callLegacyTool(ctx, "bot_tasks", encoded(q)), nil)
}
func eventArgs(trigger map[string]any) map[string]any {
	q := map[string]any{"onAny": trigger["sources"], "when": trigger["condition"], "timeZone": trigger["timeZone"]}
	for _, key := range []string{"cooldownSeconds", "expiresSeconds"} {
		if v, ok := trigger[key]; ok {
			q[key] = v
		}
	}
	return q
}
func (r *Runtime) callCompactSchedule(ctx context.Context, name string, q map[string]any) api.ToolResult {
	kind := str(q, "type")
	if name == "bot_schedule" {
		switch kind {
		case "context":
			return compactValue(map[string]any{"ok": true, "data": r.Clock()}, false)
		case "list":
			return r.schedulePage(q)
		case "sources":
			if r.care == nil || r.careLoadErr != nil {
				return compactError("care_unavailable", errors.New("native event sources unavailable"))
			}
			return compactValue(map[string]any{"ok": true, "data": map[string]any{"sources": r.care.Sources(), "policy": r.care.Snapshot().Policy, "budget": r.care.Budget(r.now()), "status": r.care.Status(), "presenceAvailable": r.careSample().Available(), "clock": r.Clock()}}, false)
		case "test":
			args := eventArgs(q["trigger"].(map[string]any))
			args["operation"] = "test"
			args["id"] = "condition-check"
			args["label"] = "Condition check"
			args["prompt"] = "Pure condition check"
			args["event"] = q["event"]
			return compact(r.callLegacyTool(ctx, "bot_care_read", encoded(args)), nil)
		}
	}
	var out api.ToolResult
	switch kind {
	case "configure":
		out = r.callLegacyTool(ctx, "bot_care", encoded(map[string]any{"operation": "configure", "policy": q["policy"]}))
	case "remove":
		domain, id, ok := strings.Cut(str(q, "automation"), ":")
		if !ok || id == "" {
			return compactError("invalid_arguments", errors.New("use exact returned automation handle"))
		}
		legacy := "bot_reminders"
		if domain == "event" {
			legacy = "bot_care"
		} else if domain != "calendar" {
			return compactError("invalid_arguments", errors.New("unknown automation kind"))
		}
		out = r.callLegacyTool(ctx, legacy, encoded(map[string]any{"operation": "remove", "id": id}))
	case "save":
		trigger := q["trigger"].(map[string]any)
		args := map[string]any{"operation": "save", "id": q["id"], "label": q["label"], "prompt": q["prompt"]}
		if str(trigger, "type") == "event" {
			for k, v := range eventArgs(trigger) {
				args[k] = v
			}
			out = r.callLegacyTool(ctx, "bot_care", encoded(args))
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
		items = append(items, map[string]any{"automation": "calendar:" + s.ID, "kind": "calendar", "id": s.ID, "label": s.Label, "next": s.Next, "enabled": s.Enabled, "schedule": s})
	}
	if kind != "calendar" && r.care != nil && r.careLoadErr == nil {
		for _, rule := range r.care.Snapshot().Rules {
			if query != "" && !strings.Contains(strings.ToLower(rule.ID+" "+rule.Label), query) {
				continue
			}
			items = append(items, map[string]any{"automation": "event:" + rule.ID, "kind": "event", "id": rule.ID, "label": rule.Label, "rule": rule})
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
		data["careAvailable"] = r.care != nil && r.careLoadErr == nil
		if r.care != nil && r.careLoadErr == nil {
			state := r.care.Snapshot()
			data["policy"] = state.Policy
			data["budget"] = r.care.Budget(r.now())
			data["status"] = r.care.Status()
			pending := []map[string]any{}
			// Recent receipts supplement the paged arrangements without loading the
			// complete activation history into every model query.
			for _, a := range state.Activations[max(0, len(state.Activations)-20):] {
				pending = append(pending, map[string]any{"ruleId": a.RuleID, "status": a.Status, "result": a.Result, "expires": a.Expires})
			}
			data["activations"] = pending
			data["activationTotal"] = len(state.Activations)
		}
	}
	return compactValue(map[string]any{"ok": true, "data": data}, false)
}
func (r *Runtime) callCompactDesktop(ctx context.Context, name string, args map[string]any) api.ToolResult {
	r.mu.Lock()
	originalTurn := r.desktopTurn
	r.mu.Unlock()
	legacy := name
	var input map[string]any
	switch name {
	case "bot_desktop_authorize":
		input = args
	case "bot_desktop_act":
		id := str(args, "requestId")
		delete(args, "requestId")
		input = map[string]any{"requestId": id, "args": args}
	case "bot_desktop_inspect":
		q := args["request"].(map[string]any)
		kind := str(q, "type")
		if kind == "grants" {
			p, ok := r.desktopControl.(interface {
				GrantStatus(context.Context) api.ToolResult
			})
			if !ok {
				return compactError("unavailable", errors.New("desktop grant status unavailable"))
			}
			turnCtx, cancel := r.desktopCallContext(ctx)
			defer cancel()
			return p.GrantStatus(turnCtx)
		}
		delete(q, "type")
		legacy = "bot_desktop_" + map[string]string{"outline": "observe", "text": "read", "delta": "sync", "image": "capture"}[kind]
		if kind == "outline" && str(q, "continuation") != "" {
			token := str(q, "continuation")
			r.mu.Lock()
			saved, ok := r.desktopQueries[token]
			turn := r.desktopTurn
			r.mu.Unlock()
			if !ok || saved.turn != turn {
				return compactError("continuation_unavailable", errors.New("original observation unavailable; start a new bounded outline"))
			}
			_ = json.Unmarshal(saved.args, &q)
			q["continuation"] = token
		}
		input = map[string]any{"requestId": "read-" + rand.Text(), "args": q}
	case "bot_desktop_result":
		q := args["request"].(map[string]any)
		if str(q, "type") == "status" {
			if id := str(q, "requestId"); id != "" {
				return r.callLegacyTool(ctx, "bot_desktop_reconcile", encoded(map[string]any{"requestId": id}))
			}
			p, ok := r.desktopControl.(interface {
				ReadRun(context.Context, string) api.ToolResult
			})
			if !ok {
				return compactError("receipt_unavailable", errors.New("original run lookup unavailable"))
			}
			return p.ReadRun(ctx, str(q, "runId"))
		}
		legacy = "bot_desktop_cancel"
		input = map[string]any{"requestId": q["requestId"], "args": map[string]any{"run_id": q["runId"]}}
	}
	out := r.callLegacyTool(ctx, legacy, encoded(input))
	if legacy == "bot_desktop_observe" && !out.IsError {
		q := input["args"].(map[string]any)
		delete(q, "continuation")
		remember := func(token string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.desktopTurn != originalTurn || r.desktopContext == nil || r.desktopContext.Err() != nil {
				return
			}
			if r.desktopQueries == nil {
				r.desktopQueries = map[string]desktopQuery{}
			}
			if len(r.desktopQueries) < 512 {
				r.desktopQueries[token] = desktopQuery{r.desktopTurn, encoded(q)}
			}
		}
		scanContinuation(out.StructuredContent, remember)
	}
	return out
}

type desktopQuery struct {
	turn string
	args json.RawMessage
}

func scanContinuation(value any, save func(string)) {
	switch v := value.(type) {
	case map[string]any:
		for k, child := range v {
			if k == "continuation" {
				if token, ok := child.(string); ok && token != "" {
					save(token)
				}
			} else {
				scanContinuation(child, save)
			}
		}
	case []any:
		for _, child := range v {
			scanContinuation(child, save)
		}
	}
}

// Keep the legacy view private to the original versioned invocation binding.
type legacyTools struct{ runtime *Runtime }

func (v legacyTools) Definitions() []api.ToolDefinition { return v.runtime.LegacyDefinitions() }
func (v legacyTools) CallTool(ctx context.Context, name string, args json.RawMessage) api.ToolResult {
	return v.runtime.callLegacyTool(ctx, name, args)
}
func (r *Runtime) LegacyToolConnection() *api.ToolConnection {
	approved := botpolicy.LegacyApprovedTools()
	if r.desktopControl != nil {
		for _, d := range desktopcontrol.LegacyDefinitions() {
			if d.Name != "bot_desktop_authorize" {
				approved = append(approved, d.Name)
			}
		}
	}
	return &api.ToolConnection{Host: legacyTools{r}, ApprovedTools: approved}
}
