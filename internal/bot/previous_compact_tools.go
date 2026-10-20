package bot

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
)

// previousCompactTools handles only the exact 5903254d Caelis catalog digest.
// It cannot be advertised by the current model or used for arbitrary schemas.
type previousCompactTools struct{ runtime *Runtime }

func (v previousCompactTools) Definitions() []api.ToolDefinition {
	r := v.runtime
	out := previousCompactDefinitions()
	if p, ok := r.engine.(api.ApplicationCapabilityProvider); ok {
		caps := p.ApplicationCapabilities()
		out = slices.DeleteFunc(out, func(d api.ToolDefinition) bool {
			return !caps.WorkerExecution && (d.Name == "bot_tasks" || d.Name == "bot_delegate" || d.Name == "bot_interactions") || !caps.ScheduledActivation && d.Name == "bot_schedule_update"
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
func (v previousCompactTools) CallTool(ctx context.Context, name string, raw json.RawMessage) api.ToolResult {
	r := v.runtime
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
	if name != "bot_dream" && r.handoff != nil && r.handoff.pending().CallID != "" {
		return compactError("context_handoff", errors.New("Bot context handoff is completing; retry after the new session is ready"))
	}
	var def *api.ToolDefinition
	for _, d := range v.Definitions() {
		if d.Name == name {
			def = &d
			break
		}
	}
	if def == nil {
		return compactError("unavailable", errors.New("historical tool unavailable"))
	}
	args, err := tc.Decode(def.InputSchema, raw)
	if err != nil {
		return compactError("invalid_arguments", err)
	}
	switch name {
	case "bot_memory":
		return compactError("retired", errors.New("Bot memory tool has been retired; no effect was performed"))
	case "bot_dream":
		return r.callDreamTool(ctx, args)
	case "bot_gesture":
		return compact(r.callLegacyTool(ctx, name, raw), nil)
	case "bot_delegate", "bot_tasks":
		return r.callPreviousCompactTask(ctx, name, args["request"].(map[string]any))
	case "bot_interactions":
		return r.callCompactInteractions(ctx, args["request"].(map[string]any))
	case "bot_schedule", "bot_schedule_update":
		return r.callCompactSchedule(ctx, name, args["request"].(map[string]any))
	}
	return compactError("unavailable", errors.New("unsupported historical tool"))
}

func (r *Runtime) previousCompactToolConnection() *api.ToolConnection {
	return &api.ToolConnection{Host: previousCompactTools{r}, ApprovedTools: botpolicy.PreviousCompactApprovedTools()}
}

func (r *Runtime) callPreviousCompactTask(ctx context.Context, name string, q map[string]any) api.ToolResult {
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
	case "retire":
		return compactError("unsupported", errors.New("task retirement is no longer supported; no effect, original binding retained"))
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
