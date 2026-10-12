package bot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botmemory"
)

type compactTasks struct {
	catalogFixture
	start        api.TaskStart
	sent         api.TaskMessage
	reads, stops int
}

func (f *compactTasks) StartTask(_ context.Context, in api.TaskStart) (api.Task, error) {
	f.start = in
	return api.Task{ID: "owned", Status: "working", Outcome: "accepted"}, nil
}
func (f *compactTasks) SendTask(_ context.Context, in api.TaskMessage) (api.Task, error) {
	f.sent = in
	return api.Task{ID: "owned", Status: "working", Outcome: "accepted"}, nil
}
func (f *compactTasks) ReadTask(context.Context, string) (api.Task, error) {
	f.reads++
	return api.Task{ID: "owned", Status: "completed", Outcome: "accepted", Result: "verified fixture"}, nil
}
func (f *compactTasks) ReadTaskRequest(ctx context.Context, id string) (api.Task, error) {
	if id != f.start.RequestID {
		return api.Task{}, context.Canceled
	}
	return f.ReadTask(ctx, "owned")
}
func (f *compactTasks) StopTask(context.Context, string) (api.Task, error) {
	f.stops++
	return api.Task{ID: "owned", Status: "working"}, nil
}
func (*compactTasks) TaskMachines() []api.TaskMachine {
	return []api.TaskMachine{{ID: "remote-one", Name: "Fedora", Ready: true}, {ID: "remote-two", Name: "Other", Ready: false}}
}
func invokeCompact(t *testing.T, r *Runtime, name, raw string) api.ToolResult {
	t.Helper()
	out := r.CallTool(t.Context(), name, json.RawMessage(raw))
	if out.IsError {
		t.Fatalf("%s: %v", name, out.Content)
	}
	return out
}
func TestCompactTaskFlowsAndInvalidBranches(t *testing.T) {
	r, _, _ := fixture(t)
	f := &compactTasks{}
	r.ConfigureTasks(f, nil)
	invokeCompact(t, r, "bot_delegate", `{"request":{"type":"start","requestId":"start-once","title":"Review","prompt":"Review assigned project"}}`)
	out := invokeCompact(t, r, "bot_tasks", `{"request":{"type":"read","requestId":"start-once"}}`)
	if out.StructuredContent["data"].(map[string]any)["status"] != "completed" {
		t.Fatal("accepted confused with completion")
	}
	invokeCompact(t, r, "bot_delegate", `{"request":{"type":"continue","task":"owned","requestId":"continue-once","prompt":"Check result"}}`)
	if f.sent.ID != "owned" || f.sent.RequestID != "continue-once" || f.start.Machine != "" {
		t.Fatal("binding overwritten")
	}
	out = invokeCompact(t, r, "bot_tasks", `{"request":{"type":"machines","query":"fedora"}}`)
	if len(out.StructuredContent["data"].([]api.TaskMachine)) != 1 {
		t.Fatal("machine filtering")
	}
	for _, action := range []string{"pin", "unpin", "lock", "unlock", "clear"} {
		raw := `{"request":{"type":"watchlist","action":"` + action + `"`
		if action != "clear" {
			raw += `,"task":"owned"`
		}
		invokeCompact(t, r, "bot_tasks", raw+`}}`)
	}
	if f.stops != 0 {
		t.Fatal("watchlist stopped work")
	}
	invokeCompact(t, r, "bot_tasks", `{"request":{"type":"stop","task":"owned"}}`)
	if f.stops != 1 {
		t.Fatal("stop missing")
	}
	before := f.calls
	for _, raw := range []string{`{"request":{"type":"list","limit":51}}`, `{"request":{"type":"read","task":"owned","requestId":"start-once"}}`, `{"request":{"type":"watchlist","action":"clear","task":"owned"}}`, `{"request":{"type":"list","runtime":"codex"}}`, `{"request":{"type":"list","type":"stop","task":"owned"}}`} {
		if !r.CallTool(t.Context(), "bot_tasks", json.RawMessage(raw)).IsError {
			t.Fatal("invalid branch accepted", raw)
		}
	}
	for _, raw := range []string{`{"request":{"type":"continue","task":"owned","requestId":"new-direction","prompt":"x","runtime":"caelis"}}`, `{"request":{"type":"continue","task":"owned","requestId":"new-direction","prompt":"x","machine":"remote"}}`, `{"request":{"type":"start","requestId":"small","title":"x","prompt":"x"}}`} {
		if !r.CallTool(t.Context(), "bot_delegate", json.RawMessage(raw)).IsError {
			t.Fatal("invalid delegation")
		}
	}
	if f.calls != before {
		t.Fatal("invalid query reached manager")
	}
}
func TestCompactScheduleNativeGrantsAndNamespaces(t *testing.T) {
	r, base, now := fixture(t)
	configureCare(t, r)
	grant := &careGrantEngine{fakeEngine: base}
	r.engine = grant
	calendar := `{"request":{"type":"save","id":"same-id","label":"Calendar","prompt":"Reminder","trigger":{"type":"calendar","timeZone":"UTC","schedule":{"type":"interval","everyMinutes":1}}}}`
	event := `{"request":{"type":"save","id":"same-id","label":"Event","prompt":"Care","trigger":{"type":"event","sources":["clock.minute"],"condition":"true","timeZone":"UTC"}}}`
	invokeCompact(t, r, "bot_schedule_update", calendar)
	if grant.authorized != "same-id" {
		t.Fatal("calendar native grant changed")
	}
	invokeCompact(t, r, "bot_schedule_update", event)
	if grant.authorized != "care:same-id" {
		t.Fatal("care native grant changed")
	}
	out := invokeCompact(t, r, "bot_schedule", `{"request":{"type":"list","limit":1}}`)
	data := out.StructuredContent["data"].(map[string]any)
	cursor := data["nextCursor"].(string)
	invokeCompact(t, r, "bot_schedule", `{"request":{"type":"list","limit":1,"cursor":"`+cursor+`"}}`)
	if !r.CallTool(t.Context(), "bot_schedule", json.RawMessage(`{"request":{"type":"list","kind":"calendar","cursor":"`+cursor+`"}}`)).IsError {
		t.Fatal("cursor filters not fenced")
	}
	invokeCompact(t, r, "bot_schedule", `{"request":{"type":"test","trigger":{"type":"event","sources":["clock.minute"],"condition":"true","timeZone":"UTC"},"event":{}}}`)
	if len(base.submissions) != 0 {
		t.Fatal("pure test dispatched")
	}
	invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"remove","automation":"calendar:same-id"}}`)
	if len(r.State().Schedules) != 0 || len(r.care.Snapshot().Rules) != 1 {
		t.Fatal("namespace collision")
	}
	invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"remove","automation":"event:same-id"}}`)
	if len(r.care.Snapshot().Rules) != 0 {
		t.Fatal("event remove")
	}
	grant.deny = true
	if !r.CallTool(t.Context(), "bot_schedule_update", json.RawMessage(calendar)).IsError || len(r.State().Schedules) != 0 {
		t.Fatal("denied schedule saved")
	}
	*now = now.Add(time.Minute)
	r.Tick(t.Context())
	if len(base.submissions) != 0 {
		t.Fatal("ungranted dispatch")
	}
	for _, raw := range []string{`{"request":{"type":"context","prompt":"write"}}`, `{"request":{"type":"test","trigger":{"type":"event","sources":["clock.minute"],"condition":"true","timeZone":"UTC"},"event":{},"policy":{}}}`} {
		if !r.CallTool(t.Context(), "bot_schedule", json.RawMessage(raw)).IsError {
			t.Fatal("read gained write")
		}
	}
}
func TestCompactMemoryAndGesture(t *testing.T) {
	r, _, _ := fixture(t)
	store, e := botmemory.Open(t.Context(), filepath.Join(t.TempDir(), "personal"), r.State().ID)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	r.ConfigurePersonal(store)
	invokeCompact(t, r, "bot_memory", `{"operation":"remember","requestId":"compact-memory","text":"Synthetic preference: concise"}`)
	out := invokeCompact(t, r, "bot_memory", `{"operation":"recall","query":"Synthetic preference"}`)
	var memory api.PersonalMemory
	b, _ := json.Marshal(out.StructuredContent["data"])
	json.Unmarshal(b, &memory)
	if len(memory.Evidence) != 1 {
		t.Fatal("memory lost")
	}
	id := memory.Evidence[0].ID
	invokeCompact(t, r, "bot_memory", `{"operation":"correct","requestId":"compact-correct","id":"`+id+`","text":"Synthetic preference: short"}`)
	out = invokeCompact(t, r, "bot_memory", `{"operation":"recall","query":"Synthetic preference"}`)
	b, _ = json.Marshal(out.StructuredContent["data"])
	json.Unmarshal(b, &memory)
	invokeCompact(t, r, "bot_memory", `{"operation":"forget","requestId":"compact-forget","id":"`+memory.Evidence[0].ID+`"}`)
	calls := 0
	r.action = func(string) error { calls++; return nil }
	for _, a := range []string{"attention", "nod", "celebrate"} {
		invokeCompact(t, r, "bot_gesture", `{"action":"`+a+`"}`)
	}
	if calls != 3 {
		t.Fatal("gesture lost")
	}
}
func TestCompactTransportUncertaintyKeepsOriginalRecovery(t *testing.T) {
	for _, test := range []struct{ name, args, next string }{
		{"bot_delegate", `{"request":{"type":"start","requestId":"original-delegate","title":"Test","prompt":"Test"}}`, "bot_tasks"},
	} {
		out := forwardFailure(toolRequest{Version: 2, Name: test.name, Arguments: json.RawMessage(test.args)}, "Connection interrupted")
		if !out.IsError || out.StructuredContent["outcome"] != "unknown" {
			t.Fatal("transport uncertainty lost", out)
		}
		next := out.StructuredContent["next"].(map[string]any)
		if next["tool"] != test.next || !strings.HasPrefix(next["request"].(map[string]any)["requestId"].(string), "original-") {
			t.Fatal("original recovery lost", next)
		}
	}
}

func TestCompactBudgetPreservesMutationReceipt(t *testing.T) {
	out := compactValue(map[string]any{"ok": true, "outcome": "accepted", "data": map[string]any{"id": "original-task", "status": "working", "result": strings.Repeat("x", 40000)}, "next": map[string]any{"tool": "bot_tasks", "request": map[string]any{"type": "read", "task": "original-task"}}}, false)
	if !out.IsError || out.StructuredContent["nativeOK"] != true || out.StructuredContent["outcome"] != "accepted" || out.StructuredContent["data"].(map[string]any)["id"] != "original-task" {
		t.Fatal("budget lost original native receipt")
	}
	b, _ := json.Marshal(out)
	if len(b) > 32<<10 {
		t.Fatal("unbounded fallback")
	}
}
