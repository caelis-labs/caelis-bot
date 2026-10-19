package bot

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/eventplan"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
)

// EventTools is the host's generation-fenced plugin relay, not a model-owned
// arbitrary MCP endpoint. The desktop injects it into its resident Bot only.
type EventTools interface {
	EventBindings() map[string]string
	CollectEvent(context.Context, string, string, string, map[string]any) (json.RawMessage, error)
}

func (r *Runtime) ConfigureEvents(tools EventTools, directory string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventTools = tools
	r.eventDirectory = directory
}
func (r *Runtime) initEvents() {
	r.events, r.eventsErr = eventplan.Open(filepath.Join(filepath.Dir(r.path), "events-"+r.provider+".json"), r.runEvent, r.now())
}
func (r *Runtime) runEvent(ctx context.Context, v eventplan.Event) (json.RawMessage, error) {
	r.mu.Lock()
	tools, dir, tasks := r.eventTools, r.eventDirectory, r.tasks
	r.mu.Unlock()
	switch v.Kind {
	case "wake":
		return json.RawMessage(`{}`), nil
	case "script":
		if runtime.GOOS == "windows" {
			return nil, errors.New("script collectors unavailable on this platform")
		}
		return eventplan.RunScript(ctx, v, dir)
	case "tool":
		if tools == nil {
			return nil, errors.New("event tool source unavailable")
		}
		return tools.CollectEvent(ctx, v.Server, v.Binding, v.Tool, v.Arguments)
	case "task":
		if tasks == nil {
			return nil, errors.New("task source unavailable")
		}
		if source, ok := tasks.(api.TaskExecutionSource); ok {
			t, generation, err := source.ObserveTaskExecution(v.Task)
			if err != nil {
				return nil, err
			}
			if v.TaskExecution == "" || generation != v.TaskExecution {
				return nil, errors.New("original task execution changed; verify original receipt")
			}
			return json.Marshal(map[string]any{"task": t.ID, "status": t.Status, "outcome": t.Outcome})
		}
		return nil, errors.New("original task unavailable")
	}
	return nil, errors.New("unsupported collector")
}
func eventDefinition() api.ToolDefinition {
	schema := tc.Schema{"type": "object", "additionalProperties": true, "description": "Your tested output contract, not an assumed tool guarantee. JSON Schema subset: type, properties, required, additionalProperties boolean, items, description. Root object; max 8 KiB."}
	base := func() tc.Schema {
		return tc.Schema{"id": text(64), "label": text(160), "context": text(4096), "outputSchema": schema}
	}
	script := base()
	script["interpreter"] = tc.String("Absolute interpreter executable; receives the saved inline script on stdin, no shell wrapper. Native execution under this reviewed registration, not a sandbox.")
	script["script"] = text(16384)
	tool := base()
	tool["server"] = tc.String("Exact server key returned by bot_schedule sources.eventTools")
	tool["tool"] = text(256)
	tool["arguments"] = tc.Schema{"type": "object", "additionalProperties": true}
	task := tc.Schema{"id": text(64), "label": text(160), "context": text(4096), "task": text(256)}
	return tc.Definition("bot_event", "Register a reusable observation for future Bot wakeups. First debug the tool/script and declare its actual JSON output contract. Registration does not execute it. Script/tool registrations grant the exact bounded native collector, so use only authorized observation work; never bypass a denied tool or sandbox. Read bot-core events guidance. Stable IDs are immutable; archive unused events. Use schedule tools for timing and optional CEL.", tc.Request(
		tc.Branch("script", script, "id", "label", "interpreter", "script", "outputSchema"),
		tc.Branch("tool", tool, "id", "label", "server", "tool", "arguments", "outputSchema"),
		tc.Branch("task", task, "id", "label", "task"),
		tc.Branch("read", tc.Schema{"event": text(128)}, "event"),
		tc.Branch("archive", tc.Schema{"event": text(128)}, "event")))
}
func scheduleDefinitions() []api.ToolDefinition { return scheduleDefinitionsWithPurpose(true) }

func scheduleDefinitionsWithPurpose(purpose bool) []api.ToolDefinition {
	out := legacyScheduleDefinitions()
	// Add simple, top-level variants without changing persisted legacy callbacks.
	for i := range out {
		var schema tc.Schema
		_ = json.Unmarshal(out[i].InputSchema, &schema)
		props := schema["properties"].(map[string]any)
		req := props["request"].(map[string]any)
		branches := req["anyOf"].([]any)
		if out[i].Name == "bot_schedule" {
			for _, branch := range branches {
				props := branch.(map[string]any)["properties"].(map[string]any)
				if purpose && props["type"].(map[string]any)["const"] == "test" {
					props["trigger"].(map[string]any)["properties"].(map[string]any)["purpose"] = tc.Enum("task", "care")
				}
			}
			branches = append(branches, tc.Branch("events", tc.Schema{"archived": tc.Schema{"type": "boolean"}}), tc.Branch("arrangements", tc.Schema{"archived": tc.Schema{"type": "boolean"}}), tc.Branch("read", tc.Schema{"handle": text(256)}, "handle"), tc.Branch("condition", tc.Schema{"event": text(128), "when": text(2048), "sample": tc.Schema{"type": "object", "additionalProperties": true}, "timeZone": text(128)}, "event", "when", "sample"))
			out[i].Description = "Read time (context), available observation sources (sources), registered events (events), active arrangements (arrangements), original result (read), or test CEL against sample data (condition). Reads never execute a collector or wake Bot. History is opt-in with archived:true; keep attention on active work. See bot-core events guide."
		} else {
			for _, branch := range branches {
				b := branch.(map[string]any)
				props := b["properties"].(map[string]any)
				if purpose && props["type"].(map[string]any)["const"] == "save" {
					triggers := props["trigger"].(map[string]any)["anyOf"].([]any)
					for _, trigger := range triggers {
						fields := trigger.(map[string]any)["properties"].(map[string]any)
						if fields["type"].(map[string]any)["const"] == "event" {
							fields["purpose"] = tc.Enum("task", "care")
						}
					}
				}
			}
			for _, kind := range []string{"after", "at", "interval", "completion"} {
				fields := tc.Schema{"id": text(64), "event": tc.String("event:<id>; omit for a plain timed Bot wakeup"), "label": text(160), "context": tc.String("Purpose / next step when creating a plain wakeup; no repeated prompt is needed for a registered event"), "when": text(2048), "timeZone": text(128), "until": tc.String("Optional RFC3339 expiry; recurring/completion default 30 days"), "cooldownSeconds": tc.Integer(60, 31622400)}
				required := []string{"id"}
				if kind == "at" {
					fields["at"] = text(128)
					required = append(required, "at")
				} else if kind != "completion" {
					fields["seconds"] = tc.Integer(1, 31622400)
					required = append(required, "seconds")
				} else {
					required = append(required, "event")
				}
				branches = append(branches, tc.Branch(kind, fields, required...))
			}
			out[i].Description = "Arrange future Bot attention: after(seconds), at(timestamp), interval(seconds), or completion(original task event). Supply an event handle for collection; optional when is CEL over event output. Omit event for a simple wakeup with label/context. Default condition true. No model polls: only a matching result wakes Bot. Stable id preserves deadlines and receipts. Completion/expiry leaves active lists automatically. Remove the returned schedule handle when work ends. Existing save/configure requests remain supported."
			if purpose {
				out[i].Description += " Native event saves default to purpose task; set trigger.purpose care only for discretionary proactive care that should wait for desktop presence. Explicit tasks do not require desktop unlock."
			}
		}
		req["anyOf"] = branches
		out[i].InputSchema, _ = json.Marshal(schema)
	}
	return out
}
func (r *Runtime) eventError() error {
	if r.eventsErr != nil || r.events == nil {
		return errors.New("event store unavailable; existing data preserved")
	}
	return nil
}
func (r *Runtime) authorizeEvent(ctx context.Context, id string, v any) error {
	if b, ok := r.engine.(api.BackgroundRuntime); ok {
		raw, _ := json.Marshal(v)
		return b.AuthorizeBackground(ctx, id, string(raw))
	}
	return nil
}
func (r *Runtime) callEvent(ctx context.Context, q map[string]any) api.ToolResult {
	if err := r.eventError(); err != nil {
		return compactError("events_unavailable", err)
	}
	kind := str(q, "type")
	id := strings.TrimPrefix(str(q, "event"), "event:")
	if kind == "read" {
		v, err := r.events.Event(id)
		if err != nil {
			return compactError("event_unavailable", err)
		}
		return compactValue(map[string]any{"ok": true, "event": "event:" + v.ID, "data": v}, false)
	}
	if kind == "archive" {
		err := r.events.ArchiveEvent(id, r.now())
		if err == nil {
			if b, ok := r.engine.(api.BackgroundRuntime); ok {
				err = b.RevokeBackground(ctx, "event:"+id)
			}
		}
		if err != nil {
			return compactError("archive_unconfirmed", err)
		}
		return compactValue(map[string]any{"ok": true}, false)
	}
	raw, _ := json.Marshal(q)
	var v eventplan.Event
	_ = json.Unmarshal(raw, &v)
	v.Kind = kind
	if kind == "script" {
		if runtime.GOOS == "windows" {
			return compactError("unavailable", errors.New("script collectors are currently macOS only"))
		}
		var err error
		v, err = eventplan.PinScriptContext(ctx, v)
		if err != nil {
			return compactError("invalid_arguments", err)
		}
	} else if kind == "tool" {
		r.mu.Lock()
		tools := r.eventTools
		r.mu.Unlock()
		if tools == nil {
			return compactError("unavailable", errors.New("no event tool connections"))
		}
		v.Binding = tools.EventBindings()[v.Server]
	} else if kind == "task" {
		v.OutputSchema = json.RawMessage(`{"type":"object","properties":{"task":{"type":"string"},"status":{"type":"string"},"outcome":{"type":"string"}},"required":["task","status","outcome"]}`)
		r.mu.Lock()
		tasks := r.tasks
		r.mu.Unlock()
		source, ok := tasks.(api.TaskExecutionSource)
		if !ok {
			return compactError("unavailable", errors.New("task execution identity unavailable"))
		}
		_, generation, err := source.ObserveTaskExecution(v.Task)
		if err != nil || generation == "" {
			return compactError("unavailable", errors.New("original owned task execution unavailable"))
		}
		v.TaskExecution = generation
	}
	saved, err := r.events.Register(v, r.now(), func() error { return r.authorizeEvent(ctx, "event:"+v.ID, v) })
	if err != nil {
		return compactError("event_registration", err)
	}
	return compactValue(map[string]any{"ok": true, "event": "event:" + saved.ID, "outputSchema": saved.OutputSchema, "validation": "contract accepted; no collector executed", "next": map[string]any{"tool": "bot_schedule_update", "request": map[string]any{"type": "after", "id": saved.ID + "-once", "event": "event:" + saved.ID, "seconds": 60}}}, false)
}
func (r *Runtime) eventSchedule(ctx context.Context, name string, q map[string]any) (api.ToolResult, bool) {
	kind := str(q, "type")
	isNew := name == "bot_schedule" && (kind == "events" || kind == "arrangements" || kind == "read" || kind == "condition") || name == "bot_schedule_update" && (kind == "after" || kind == "at" || kind == "interval" || kind == "completion" || kind == "remove" && strings.HasPrefix(str(q, "automation"), "schedule:"))
	if !isNew {
		return api.ToolResult{}, false
	}
	fail := func(err error) (api.ToolResult, bool) { return compactError("event_arrangement", err), true }
	if err := r.eventError(); err != nil {
		return fail(err)
	}
	if name == "bot_schedule" {
		s := r.events.Snapshot()
		history, _ := q["archived"].(bool)
		switch kind {
		case "events":
			items := []map[string]any{}
			for _, v := range s.Events {
				if history != v.Archived.IsZero() {
					items = append(items, map[string]any{"event": "event:" + v.ID, "label": v.Label, "kind": v.Kind, "archived": v.Archived})
				}
			}
			return compactValue(map[string]any{"ok": true, "data": items}, false), true
		case "arrangements":
			items := []map[string]any{}
			for _, p := range s.Plans {
				if history != p.Finished.IsZero() {
					items = append(items, map[string]any{"automation": "schedule:" + p.ID, "event": "event:" + p.Event, "type": p.Kind, "next": p.Next, "expires": p.Expires, "status": p.Status, "issue": p.Issue, "latestOccurrence": latestOccurrence(s, p.ID)})
				}
			}
			return compactValue(map[string]any{"ok": true, "data": items}, false), true
		case "condition":
			v, err := r.events.Event(strings.TrimPrefix(str(q, "event"), "event:"))
			if err != nil {
				return fail(err)
			}
			raw, _ := json.Marshal(q["sample"])
			data, err := eventplan.ValidateOutput(v.OutputSchema, raw)
			if err != nil {
				return fail(err)
			}
			match, err := eventplan.Test(eventplan.Plan{When: str(q, "when"), TimeZone: str(q, "timeZone")}, data, r.now())
			if err != nil {
				return fail(err)
			}
			return compactValue(map[string]any{"ok": true, "wouldWake": match}, false), true
		case "read":
			h := str(q, "handle")
			for _, p := range s.Plans {
				if h == "schedule:"+p.ID {
					p.OriginRequests = nil
					return compactValue(map[string]any{"ok": true, "data": p, "latestOccurrence": latestOccurrence(s, p.ID)}, false), true
				}
			}
			for _, o := range s.Occurrences {
				if h == o.ID {
					return compactValue(map[string]any{"ok": true, "data": o}, false), true
				}
			}
			return fail(errors.New("handle unavailable or history retention elapsed"))
		}
	}
	if kind == "remove" {
		id := strings.TrimPrefix(str(q, "automation"), "schedule:")
		if err := r.events.Remove(id, r.now()); err != nil {
			return fail(err)
		}
		if b, ok := r.engine.(api.BackgroundRuntime); ok {
			if err := b.RevokeBackground(ctx, "schedule:"+id); err != nil {
				return fail(err)
			}
		}
		return compactValue(map[string]any{"ok": true}, false), true
	}
	raw, _ := json.Marshal(q)
	var p eventplan.Plan
	_ = json.Unmarshal(raw, &p)
	p.OriginRequests = r.scheduleOrigin(ctx)
	p.Kind = kind
	p.Event = strings.TrimPrefix(p.Event, "event:")
	if p.Event == "" {
		label := str(q, "label")
		if label == "" {
			label = p.ID
		}
		purpose := str(q, "context")
		if purpose == "" {
			return fail(errors.New("plain wakeup needs context describing the intended follow-up"))
		}
		v := eventplan.Event{ID: p.ID, Label: label, Context: purpose, Kind: "wake", OutputSchema: json.RawMessage(`{"type":"object"}`)}
		if _, err := r.events.Register(v, r.now()); err != nil {
			return fail(err)
		}
		p.Event = v.ID
	}
	// Fingerprint the original relative specification, not wall-clock-derived Next.
	saved, err := r.events.Save(p, r.now(), func() error { return r.authorizeEvent(ctx, "schedule:"+p.ID, p) })
	if err != nil {
		return fail(err)
	}
	return compactValue(map[string]any{"ok": true, "automation": "schedule:" + saved.ID, "event": "event:" + saved.Event, "next": saved.Next, "expires": saved.Expires, "status": saved.Status}, false), true
}
func (r *Runtime) tickEvents(ctx context.Context) error {
	if r.events == nil {
		return nil
	}
	// Explicit arrangements can run while the desktop is locked or presence is
	// unknown. A collector may observe presence and its CEL may require it; that
	// is task data, not a global admission policy. The adapter rechecks busy state.
	origins := map[string][]string{}
	for _, p := range r.events.Snapshot().Plans {
		origins[p.ID] = p.OriginRequests
	}
	ready := r.engine.Snapshot().CanSend
	return r.events.Tick(ctx, r.now(), eventplan.Delivery{Ready: ready,
		Revoke: func(id string) error {
			if b, ok := r.engine.(api.BackgroundRuntime); ok {
				return b.RevokeBackground(ctx, id)
			}
			return nil
		},
		Submit: func(ctx context.Context, id, text, plan string) (string, error) {
			var receipt api.Receipt
			var err error
			in := api.Submission{ID: id, Text: text, Scheduled: true, OriginRequests: origins[plan]}
			if b, ok := r.engine.(api.BackgroundRuntime); ok {
				receipt, err = b.SubmitBackground(ctx, in, []string{"schedule:" + plan})
			} else {
				receipt, err = r.engine.Submit(ctx, in, nil)
			}
			if receipt.ID != id {
				return "unknown", errors.New("wake receipt identity mismatch")
			}
			if err == nil && receipt.Outcome == "rejected" && receipt.Deferred {
				return "deferred", nil
			}
			return receipt.Outcome, err
		}, Receipt: func(id string) string {
			receipt := r.backgroundReceipt(id)
			if receipt.ID != id {
				return "unknown"
			}
			return receipt.Outcome
		}, Complete: func(id string) bool {
			if b, ok := r.engine.(api.BackgroundResultProvider); ok {
				result := b.BackgroundResult(id)
				return result.ID == id && result.Complete
			}
			return false
		}})
}
func (r *Runtime) eventBindings() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.eventTools == nil {
		return map[string]string{}
	}
	return r.eventTools.EventBindings()
}

func latestOccurrence(s eventplan.State, plan string) string {
	for i := len(s.Occurrences) - 1; i >= 0; i-- {
		if s.Occurrences[i].Plan == plan {
			return s.Occurrences[i].ID
		}
	}
	return ""
}
