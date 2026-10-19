package bot

import (
	"context"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"strings"
	"testing"
	"time"
)

type eventFixtureTools struct{ calls int }

func (*eventFixtureTools) EventBindings() map[string]string {
	return map[string]string{"fixture/mail": "v1"}
}
func (f *eventFixtureTools) CollectEvent(context.Context, string, string, string, map[string]any) (json.RawMessage, error) {
	f.calls++
	return json.RawMessage(`{"count":23}`), nil
}
func TestRegisteredToolArrangementUsesNativeGrantAndNaturalWake(t *testing.T) {
	r, base, now := fixture(t)
	configureNativeSources(t, r)
	grant := &scheduleGrantEngine{fakeEngine: base}
	r.engine = grant
	tools := &eventFixtureTools{}
	r.ConfigureEvents(tools, t.TempDir())
	invokeCompact(t, r, "bot_event", `{"request":{"type":"tool","id":"mail","label":"Unread mail","context":"Organize mail when there is enough to review","server":"fixture/mail","tool":"read","arguments":{},"outputSchema":{"type":"object","properties":{"count":{"type":"integer"}},"required":["count"]}}}`)
	if tools.calls != 0 || grant.authorized != "event:mail" {
		t.Fatal("registration executed or ungranted")
	}
	out := invokeCompact(t, r, "bot_schedule", `{"request":{"type":"condition","event":"event:mail","when":"event.count >= 20","sample":{"count":23}}}`)
	if out.StructuredContent["wouldWake"] != true || tools.calls != 0 {
		t.Fatal(out)
	}
	invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"after","id":"mail-check","event":"event:mail","seconds":10,"when":"event.count >= 20"}}`)
	if grant.authorized != "schedule:mail-check" {
		t.Fatal(grant.authorized)
	}
	*now = now.Add(10 * time.Second)
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Collector completion is asynchronous; no idle model loop.
	deadline := time.Now().Add(time.Second)
	for len(base.submissions) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		if err := r.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(base.submissions) != 1 || !base.submissions[0].Scheduled || !strings.Contains(base.submissions[0].Text, "Unread mail") || !strings.Contains(base.submissions[0].Text, "23") || !strings.Contains(base.submissions[0].Text, api.SilentReminder) {
		t.Fatal(base.submissions)
	}
	if tools.calls != 1 {
		t.Fatal("collector repeated")
	}
	invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"remove","automation":"schedule:mail-check"}}`)
}
func TestSimpleWakeIsOneSmallCallAndCancelledArrangementLeavesActiveList(t *testing.T) {
	r, base, now := fixture(t)
	raw := `{"request":{"type":"after","id":"follow-up","seconds":600,"context":"Continue the requested review"}}`
	first := invokeCompact(t, r, "bot_schedule_update", raw)
	*now = now.Add(time.Minute)
	second := invokeCompact(t, r, "bot_schedule_update", raw)
	if first.StructuredContent["next"] != second.StructuredContent["next"] {
		t.Fatal("retry postponed wake")
	}
	invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"remove","automation":"schedule:follow-up"}}`)
	active := invokeCompact(t, r, "bot_schedule", `{"request":{"type":"arrangements"}}`)
	archived := invokeCompact(t, r, "bot_schedule", `{"request":{"type":"arrangements","archived":true}}`)
	if len(active.StructuredContent["data"].([]map[string]any)) != 0 || len(archived.StructuredContent["data"].([]map[string]any)) != 1 {
		t.Fatal(active, archived)
	}
	*now = now.Add(time.Hour)
	_ = r.Tick(t.Context())
	if len(base.submissions) != 0 {
		t.Fatal("cancelled wake fired")
	}
}
func TestEventRegistrationDeniedDoesNotPersistAndReadCannotExecute(t *testing.T) {
	r, base, _ := fixture(t)
	r.engine = &scheduleGrantEngine{fakeEngine: base, deny: true}
	r.ConfigureEvents(&eventFixtureTools{}, t.TempDir())
	raw := `{"request":{"type":"tool","id":"denied","label":"Denied","server":"fixture/mail","tool":"read","arguments":{},"outputSchema":{"type":"object"}}}`
	if !r.CallTool(t.Context(), "bot_event", json.RawMessage(raw)).IsError || len(r.events.Snapshot().Events) != 0 {
		t.Fatal("denied event registered")
	}
	if !r.CallTool(t.Context(), "bot_schedule", json.RawMessage(raw)).IsError {
		t.Fatal("read executed registration")
	}
}

func TestEventCatalogIsReviewedAndOlderCatalogsStayStable(t *testing.T) {
	r, _, _ := fixture(t)
	current := false
	for _, d := range r.Definitions() {
		current = current || d.Name == "bot_event"
	}
	if !current {
		t.Fatal("event registration undiscoverable")
	}
	for _, d := range (priorEventTools{r}).Definitions() {
		if d.Name == "bot_event" || d.Name == "bot_schedule_update" && strings.Contains(string(d.InputSchema), `"after"`) {
			t.Fatal("historical catalog gained event execution")
		}
	}
	for _, d := range (priorQuestionTools{r}).Definitions() {
		if d.Name == "bot_event" {
			t.Fatal("older question catalog changed")
		}
	}
}

func TestEventUnknownWakeCannotUseNewHumanReceipt(t *testing.T) {
	r, base, now := fixture(t)
	configureNativeSources(t, r)
	base.outcome = "unknown"
	invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"after","id":"unknown-wake","seconds":1,"context":"Follow up the original goal"}}`)
	*now = now.Add(time.Second)
	deadline := time.Now().Add(time.Second)
	for len(base.submissions) == 0 && time.Now().Before(deadline) {
		if err := r.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if len(base.submissions) != 1 {
		t.Fatal("no original wake")
	}
	original := base.submissions[0].ID
	base.view.LastReceipt = api.Receipt{ID: "new-human-message", Outcome: "accepted"}
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	s := r.events.Snapshot()
	if len(s.Occurrences) != 1 || s.Occurrences[0].ID != original || s.Occurrences[0].Status != "unknown_wake" {
		t.Fatal("unrelated receipt settled original wake", s)
	}
}

type eventTaskFixture struct {
	compactTasks
	generation string
	status     string
}

func (f *eventTaskFixture) ObserveTaskExecution(id string) (api.Task, string, error) {
	return api.Task{ID: id, Status: f.status, Outcome: "accepted"}, f.generation, nil
}
func TestTaskEventPinsOriginalExecution(t *testing.T) {
	r, _, _ := fixture(t)
	tasks := &eventTaskFixture{generation: "E1", status: "working"}
	r.tasks = tasks
	invokeCompact(t, r, "bot_event", `{"request":{"type":"task","id":"tests","label":"Tests","task":"owned"}}`)
	v, err := r.events.Event("tests")
	if err != nil {
		t.Fatal(err)
	}
	if v.TaskExecution != "E1" {
		t.Fatal(v)
	}
	if _, err = r.runEvent(t.Context(), v); err != nil {
		t.Fatal(err)
	}
	tasks.generation = "E2"
	tasks.status = "failed"
	if result, err := r.runEvent(t.Context(), v); err == nil || len(result) != 0 {
		t.Fatal("observed continuation instead of original execution", string(result), err)
	}
}
