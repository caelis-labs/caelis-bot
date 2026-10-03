package bot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botmemory"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
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
func TestCompactDesktopLifecycleSchemaAndCatalog(t *testing.T) {
	r, _, _ := fixture(t)
	d := &desktopFixture{}
	r.ConfigureDesktopControl(d)
	r.BeginDesktopTurn()
	defs := r.Definitions()
	if len(defs) != 10 {
		t.Fatal("expected 10", len(defs))
	}
	if slices.Contains(desktopcontrol.ApprovedTools(), "bot_desktop_authorize") {
		t.Fatal("grant autoapproved")
	}
	before := d.calls
	if !r.CallTool(t.Context(), "bot_desktop_inspect", json.RawMessage(`{"request":{"type":"image","kind":"visible_region","target":"window"}}`)).IsError || d.calls != before {
		t.Fatal("image capability bypass")
	}
	invokeCompact(t, r, "bot_desktop_inspect", `{"request":{"type":"outline","scope":{"desktop":true},"fields":["name","role"]}}`)
	r.StopDesktopTurn()
	invokeCompact(t, r, "bot_desktop_result", `{"request":{"type":"status","requestId":"original-action"}}`)
	for name, raw := range map[string]string{"bot_desktop_act": `{"requestId":"next-action","steps":[{"id":"invoke","op":"invoke","target":{"ref":"button"}}]}`, "bot_desktop_result": `{"request":{"type":"cancel","requestId":"cancel-action","runId":"run-1"}}`, "bot_desktop_inspect": `{"request":{"type":"outline","scope":{"desktop":true}}}`} {
		if !r.CallTool(t.Context(), name, json.RawMessage(raw)).IsError {
			t.Fatal("ended turn bypass", name)
		}
	}
	for _, def := range defs {
		var schema map[string]any
		json.Unmarshal(def.InputSchema, &schema)
		if schema["type"] != "object" {
			t.Fatal("invalid root")
		}
	}
	// Check actual catalog cost without estimating tokenizer-specific counts.
	newJSON, _ := json.Marshal(defs)
	oldJSON, _ := json.Marshal(r.LegacyDefinitions())
	t.Logf("catalog: 22 -> 10 tools; definition bytes %d -> %d", len(oldJSON), len(newJSON))
	for _, bad := range []string{`{"request":{"type":"outline","scope":{"desktop":true},"turn":"forged"}}`, `{"request":{"type":"outline","continuation":"token","scope":{"desktop":true}}}`} {
		for _, def := range defs {
			if def.Name == "bot_desktop_inspect" {
				if _, e := tc.Decode(def.InputSchema, json.RawMessage(bad)); e == nil {
					t.Fatal("forged observation")
				}
			}
		}
	}
	if strings.Contains(string(newJSON), "bot_task_start") {
		t.Fatal("old tool advertised")
	}
}

func TestCompactTransportUncertaintyKeepsOriginalRecovery(t *testing.T) {
	for _, test := range []struct{ name, args, next string }{
		{"bot_delegate", `{"request":{"type":"start","requestId":"original-delegate","title":"Test","prompt":"Test"}}`, "bot_tasks"},
		{"bot_desktop_act", `{"requestId":"original-desktop","steps":[]}`, "bot_desktop_result"},
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

type continuationFixture struct {
	queries          []map[string]any
	entered, release chan struct{}
}

func (*continuationFixture) Definitions() []api.ToolDefinition {
	return desktopcontrol.LegacyDefinitions()
}
func (d *continuationFixture) CallTool(_ context.Context, _ string, raw json.RawMessage) api.ToolResult {
	var input map[string]any
	json.Unmarshal(raw, &input)
	d.queries = append(d.queries, input["args"].(map[string]any))
	if d.entered != nil {
		close(d.entered)
		<-d.release
	}
	return api.ToolResult{StructuredContent: map[string]any{"coverage": map[string]any{"continuation": "query-token"}}}
}
func TestCompactContinuationRestoresOnlySameTurnQuery(t *testing.T) {
	r, _, _ := fixture(t)
	d := &continuationFixture{}
	r.ConfigureDesktopControl(d)
	r.BeginDesktopTurn()
	invokeCompact(t, r, "bot_desktop_inspect", `{"request":{"type":"outline","scope":{"desktop":true},"fields":["name"],"budget":{"max_results":1}}}`)
	invokeCompact(t, r, "bot_desktop_inspect", `{"request":{"type":"outline","continuation":"query-token"}}`)
	if len(d.queries) != 2 || d.queries[1]["budget"].(map[string]any)["max_results"] != float64(1) || d.queries[1]["continuation"] != "query-token" {
		t.Fatal("host failed to restore exact query")
	}
	r.BeginDesktopTurn()
	if !r.CallTool(t.Context(), "bot_desktop_inspect", json.RawMessage(`{"request":{"type":"outline","continuation":"query-token"}}`)).IsError || len(d.queries) != 2 {
		t.Fatal("continuation crossed turn")
	}
	// A late old read cannot seed the new turn's cache.
	d.entered = make(chan struct{})
	d.release = make(chan struct{})
	done := make(chan struct{})
	go func() {
		r.CallTool(t.Context(), "bot_desktop_inspect", json.RawMessage(`{"request":{"type":"outline","scope":{"desktop":true}}}`))
		close(done)
	}()
	<-d.entered
	r.BeginDesktopTurn()
	close(d.release)
	<-done
	if !r.CallTool(t.Context(), "bot_desktop_inspect", json.RawMessage(`{"request":{"type":"outline","continuation":"query-token"}}`)).IsError {
		t.Fatal("late result seeded new turn")
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

func TestInvokeValidationRejectsBeforeNativeDispatch(t *testing.T) {
	r, _, _ := fixture(t)
	d := &desktopFixture{}
	r.ConfigureDesktopControl(d)
	r.BeginDesktopTurn()
	for _, raw := range []string{`{"requestId":"invoke-correctable","steps":[{"id":"s","op":"invoke","target":{"ref":"known"},"invoke":{}}]}`, `{"requestId":"invoke-correctable","steps":[{"id":"s","op":"invoke","target":{"ref":"known"},"completion":"verify"}]}`} {
		out := r.CallTool(t.Context(), "bot_desktop_act", json.RawMessage(raw))
		if !out.IsError || out.StructuredContent["outcome"] != "rejected" || d.calls != 0 {
			t.Fatal("invalid input reached native helper", out)
		}
	}
	invokeCompact(t, r, "bot_desktop_act", `{"requestId":"invoke-correctable","steps":[{"id":"s","op":"invoke","target":{"ref":"known"},"completion":"dispatch"}]}`)
	if d.calls != 1 {
		t.Fatal("corrected original ID did not dispatch once")
	}
}
