package bot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type compactTasks struct {
	catalogFixture
	start         api.TaskStart
	sent          api.TaskMessage
	reads, stops  int
	starts, sends int
}

type uncertainCompactTasks struct{ compactTasks }

func (f *uncertainCompactTasks) StartTask(_ context.Context, in api.TaskStart) (api.Task, error) {
	f.start = in
	f.starts++
	return api.Task{ID: "owned", Status: "unknown", Outcome: "unknown"}, context.DeadlineExceeded
}
func TestUnconfirmedDelegationReturnsOriginalOperationForReconciliation(t *testing.T) {
	r, _, _ := fixture(t)
	f := &uncertainCompactTasks{}
	r.ConfigureTasks(f, nil)
	invocation := api.ToolInvocation{Provider: "codex", Session: "resident", Turn: "turn", CallID: "lost-create"}
	ctx := api.WithToolInvocation(t.Context(), invocation)
	out := r.CallTool(ctx, "bot_delegate", json.RawMessage(`{"title":"Review","prompt":"Check assigned source"}`))
	if !out.IsError {
		t.Fatal("unknown became success")
	}
	data := out.StructuredContent["data"].(map[string]any)
	if data["requestId"] != toolSubmissionID(invocation) || data["id"] != "owned" || f.starts != 1 {
		t.Fatal("original receipt is not available to reconcile", data)
	}
}

func (f *compactTasks) StartTask(_ context.Context, in api.TaskStart) (api.Task, error) {
	f.start = in
	f.starts++
	return api.Task{ID: "owned", Status: "working", Outcome: "accepted"}, nil
}
func (f *compactTasks) SendTask(_ context.Context, in api.TaskMessage) (api.Task, error) {
	f.sent = in
	f.sends++
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
func (f *compactTasks) QueryTaskHistory(q api.TaskHistoryQuery) ([]api.TaskHistoryEntry, error) {
	f.calls++
	return []api.TaskHistoryEntry{{ID: "owned", Title: "Review", Status: "working", Environment: "local", CreatedAt: "2026-10-13T10:00:00Z"}}, nil
}
func (f *compactTasks) ReadTaskConversation(context.Context, string) (api.TaskConversation, error) {
	f.reads++
	return api.TaskConversation{Status: "working", Messages: []api.TaskConversationMessage{{ID: "user-1", Role: "user", Text: "Review assigned project"}, {ID: "assistant-1", Role: "assistant", Text: "progress"}}}, nil
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
	call := func(id, raw string) api.ToolResult {
		ctx := api.WithToolInvocation(t.Context(), api.ToolInvocation{Provider: "codex", Session: "resident", Turn: "turn", CallID: id})
		return r.CallTool(ctx, "bot_delegate", json.RawMessage(raw))
	}
	out := call("start-once", `{"title":"Review","prompt":"Review assigned project"}`)
	firstID := f.start.RequestID
	if out.IsError || f.start.RequestID != toolSubmissionID(api.ToolInvocation{Provider: "codex", Session: "resident", Turn: "turn", CallID: "start-once"}) || !f.start.LocalRuntime {
		t.Fatal("native invocation did not own start", out, f.start)
	}
	if retry := call("start-once", `{"title":"Review","prompt":"Review assigned project"}`); retry.IsError || f.start.RequestID == "" {
		t.Fatal("same invocation receipt failed", retry)
	}
	if different := call("another-call", `{"title":"Review","prompt":"Review assigned project"}`); different.IsError || f.starts != 3 || f.start.RequestID == firstID {
		t.Fatal("distinct same-text invocation deduplicated", different, f.starts)
	}
	out = invokeCompact(t, r, "bot_tasks", `{"request":{"type":"read","task":"owned"}}`)
	if out.StructuredContent["data"].(map[string]any)["status"] != "working" {
		t.Fatal("read confused with completion", out)
	}
	out = call("continue-once", `{"task":"owned","prompt":"Check result"}`)
	if out.IsError || f.sent.ID != "owned" || f.sent.RequestID == "" || f.start.Machine != "" {
		t.Fatal("binding overwritten")
	}
	out = invokeCompact(t, r, "bot_tasks", `{"request":{"type":"environments"}}`)
	if len(out.StructuredContent["data"].([]map[string]string)) != 3 {
		t.Fatal("environment list", out)
	}
	invokeCompact(t, r, "bot_tasks", `{"request":{"type":"pin","task":"owned","pinned":true}}`)
	if f.stops != 0 || f.pinned != "owned" {
		t.Fatal("pin executed worker")
	}
	invokeCompact(t, r, "bot_tasks", `{"request":{"type":"stop","task":"owned"}}`)
	if f.stops != 1 {
		t.Fatal("stop missing")
	}
	before := f.calls
	for _, raw := range []string{`{"request":{"type":"list","limit":51}}`, `{"request":{"type":"read","task":"owned","requestId":"start-once"}}`, `{"request":{"type":"watchlist","action":"clear","task":"owned"}}`, `{"request":{"type":"read","task":"owned","messageId":"assistant-1","limit":1}}`, `{"request":{"type":"list","runtime":"codex"}}`, `{"request":{"type":"list","type":"stop","task":"owned"}}`} {
		if !r.CallTool(t.Context(), "bot_tasks", json.RawMessage(raw)).IsError {
			t.Fatal("invalid branch accepted", raw)
		}
	}
	for _, raw := range []string{`{"task":"owned","prompt":"x","title":"new title"}`, `{"task":"owned","prompt":"x","environment":"remote-one"}`, `{"request":{"type":"start","requestId":"small","prompt":"x"}}`} {
		if !call("invalid", raw).IsError {
			t.Fatal("invalid delegation")
		}
	}
	if !call("start-once", `{"title":"Review","prompt":"different"}`).IsError {
		t.Fatal("original invocation intent changed")
	}
	if f.calls != before {
		t.Fatal("invalid query reached manager")
	}
}

func TestPreviousCompactPendingSchemaUsesOriginalTaskHandler(t *testing.T) {
	r, _, _ := fixture(t)
	f := &compactTasks{}
	if err := r.ConfigureTasks(f, nil); err != nil {
		t.Fatal(err)
	}
	previous := previousCompactTools{r}
	start := previous.CallTool(t.Context(), "bot_delegate", json.RawMessage(`{"request":{"type":"start","requestId":"old-call-1","prompt":"original work"}}`))
	if start.IsError || f.starts != 1 || f.start.RequestID != "old-call-1" {
		t.Fatal("old start callback did not retain its request identity", start, f.start)
	}
	continued := previous.CallTool(t.Context(), "bot_delegate", json.RawMessage(`{"request":{"type":"continue","requestId":"old-call-2","task":"owned","prompt":"next work"}}`))
	if continued.IsError || f.sends != 1 || f.sent.RequestID != "old-call-2" || f.sent.ID != "owned" {
		t.Fatal("old continuation callback was rerouted", continued, f.sent)
	}
	if out := previous.CallTool(t.Context(), "bot_tasks", json.RawMessage(`{"request":{"type":"read","requestId":"old-call-1"}}`)); out.IsError || f.reads != 1 {
		t.Fatal("old receipt read was lost", out)
	}
	if out := previous.CallTool(t.Context(), "bot_memory", json.RawMessage(`{"operation":"recall"}`)); !out.IsError {
		t.Fatal("retired memory effect was admitted")
	}
	if out := r.CallTool(t.Context(), "bot_delegate", json.RawMessage(`{"request":{"type":"start","requestId":"old-call-3","prompt":"new work"}}`)); !out.IsError {
		t.Fatal("old schema leaked into the current catalog")
	}
}
func TestTaskReadUsesCompletedMessageIdentityAndUTF8ByteFolding(t *testing.T) {
	value := strings.Repeat("界", 400)
	folded := collapsedTaskText(value)
	if !utf8.ValidString(folded) || !strings.Contains(folded, "[已折叠 ") || len(folded) >= len(value) {
		t.Fatal("UTF-8 byte summary corrupted", folded)
	}
	if got := collapsedTaskText("short 文本"); got != "short 文本" {
		t.Fatal("short message altered", got)
	}
	r, _, _ := fixture(t)
	f := &compactTasks{}
	r.ConfigureTasks(f, nil)
	out := invokeCompact(t, r, "bot_tasks", `{"request":{"type":"read","task":"owned","messageId":"assistant-1"}}`)
	data := out.StructuredContent["data"].(map[string]any)
	if data["status"] != "working" || data["message"].(api.TaskConversationMessage).Text != "progress" || data["messages"] != nil {
		t.Fatal("detail repeated list fields", data)
	}
	if !r.CallTool(t.Context(), "bot_tasks", json.RawMessage(`{"request":{"type":"read","task":"owned","messageId":"missing"}}`)).IsError {
		t.Fatal("unknown native message ID became empty success")
	}
}
func TestCompactScheduleNativeGrantsAndNamespaces(t *testing.T) {
	r, base, now := fixture(t)
	configureNativeSources(t, r)
	grant := &scheduleGrantEngine{fakeEngine: base}
	r.engine = grant
	calendar := `{"request":{"type":"save","id":"same-id","label":"Calendar","prompt":"Reminder","trigger":{"type":"calendar","timeZone":"UTC","schedule":{"type":"interval","everyMinutes":1}}}}`
	event := `{"request":{"type":"save","id":"same-id","label":"Event","prompt":"Care","trigger":{"type":"event","sources":["clock.minute"],"condition":"true","timeZone":"UTC"}}}`
	invokeCompact(t, r, "bot_schedule_update", calendar)
	if grant.authorized != "same-id" {
		t.Fatal("calendar native grant changed")
	}
	invokeCompact(t, r, "bot_schedule_update", event)
	native, _ := r.events.NativePlan("same-id")
	if grant.authorized != native.Authorization() {
		t.Fatal("native schedule grant changed")
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
	if len(r.State().Schedules) != 0 || len(r.events.Snapshot().Plans) != 1 {
		t.Fatal("namespace collision")
	}
	invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"remove","automation":"event:same-id"}}`)
	if r.events.Snapshot().Plans[0].Status != "cancelled" {
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
func TestCompactGesture(t *testing.T) {
	r, _, _ := fixture(t)
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
	out := forwardFailure(toolRequest{Version: 2, Name: "bot_delegate", Arguments: json.RawMessage(`{"prompt":"Test"}`)}, "Connection interrupted")
	if !out.IsError || out.StructuredContent["outcome"] != "unknown" || out.StructuredContent["next"] != nil {
		t.Fatal("transport uncertainty suggested a new model ID", out)
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
