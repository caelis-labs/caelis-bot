package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/care"
)

func waitOccurrence(t *testing.T, r *Runtime) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := r.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
		state := r.events.Snapshot()
		if len(state.Occurrences) > 0 && state.Occurrences[0].Status != "collecting" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("collector did not finish")
}

func TestExplicitSchedulePresenceMatrix(t *testing.T) {
	yes, no := true, false
	cases := map[string]care.Sample{
		"unlocked":    {Presence: care.Presence{Awake: true, Unlocked: &yes}},
		"locked":      {Presence: care.Presence{Awake: true, Unlocked: &no}},
		"unknown":     {},
		"display_off": {Presence: care.Presence{Unlocked: &yes}},
	}
	for name, sample := range cases {
		for _, kind := range []string{"after", "at", "interval", "tool", "script", "completion", "calendar", "native_event"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				if kind == "script" && runtime.GOOS == "windows" {
					t.Skip("Windows script collectors intentionally unavailable")
				}
				r, f, now := fixture(t)
				if err := r.ConfigureCare(func() care.Sample { return sample }); err != nil {
					t.Fatal(err)
				}
				r.ConfigureEvents(&eventFixtureTools{}, t.TempDir())
				request := map[string]any{"type": "after", "id": "explicit", "seconds": 1, "context": "Deliver requested result"}
				switch kind {
				case "at":
					request["type"] = "at"
					request["at"] = now.Add(time.Minute).Format(time.RFC3339)
					delete(request, "seconds")
				case "interval":
					request["type"] = "interval"
					request["seconds"] = 60
				case "tool":
					invokeCompact(t, r, "bot_event", `{"request":{"type":"tool","id":"observe","label":"Observe","server":"fixture/mail","tool":"read","arguments":{},"outputSchema":{"type":"object"}}}`)
					request["event"] = "event:observe"
				case "script":
					invokeCompact(t, r, "bot_event", `{"request":{"type":"script","id":"observe","label":"Observe","interpreter":"/bin/sh","script":"printf '{\"ready\":true}'","outputSchema":{"type":"object"}}}`)
					request["event"] = "event:observe"
					request["when"] = "event.ready == true"
				case "completion":
					r.tasks = &eventTaskFixture{generation: "original", status: "completed"}
					invokeCompact(t, r, "bot_event", `{"request":{"type":"task","id":"observe","label":"Original task","task":"owned"}}`)
					request["type"] = "completion"
					request["event"] = "event:observe"
					delete(request, "seconds")
				case "calendar":
					saveReminder(t, r, "explicit")
				case "native_event":
					invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"save","id":"explicit","label":"Explicit event","prompt":"Deliver requested result","trigger":{"type":"event","sources":["clock.minute"],"condition":"true","timeZone":"UTC"}}}`)
				}
				if kind != "calendar" && kind != "native_event" {
					raw, _ := json.Marshal(map[string]any{"request": request})
					invokeCompact(t, r, "bot_schedule_update", string(raw))
				}
				*now = now.Add(time.Minute)
				f.view.CanSend = false
				if kind == "calendar" || kind == "native_event" {
					if err := r.Tick(t.Context()); err != nil {
						t.Fatal(err)
					}
				} else {
					waitOccurrence(t, r)
				}
				if len(f.submissions) != 0 {
					t.Fatal("busy Bot was interrupted")
				}
				f.view.CanSend = true
				for range 3 {
					if err := r.Tick(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
				if len(f.submissions) != 1 || !f.submissions[0].Scheduled {
					t.Fatalf("%s: submissions=%d", kind, len(f.submissions))
				}
			})
		}
	}
}

func TestExplicitSchedulesDoNotSamplePresence(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			r, f, now := fixture(t)
			if configured {
				if err := r.ConfigureCare(func() care.Sample { t.Fatal("explicit wake sampled desktop"); return care.Sample{} }); err != nil {
					t.Fatal(err)
				}
			}
			invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"after","id":"wake","seconds":1,"context":"Follow up"}}`)
			*now = now.Add(time.Second)
			waitOccurrence(t, r)
			if len(f.submissions) != 1 {
				t.Fatal("explicit schedule required native care")
			}
		})
	}
}

func TestExplicitEventConditionCanRequireUnlocked(t *testing.T) {
	for _, value := range []string{"true", "false", "null"} {
		t.Run(value, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("native script collectors unavailable")
			}
			r, f, now := fixture(t)
			r.ConfigureEvents(nil, t.TempDir())
			request := map[string]any{"type": "script", "id": "desktop", "label": "Task requires unlocked desktop", "interpreter": "/bin/sh", "script": `printf '{"unlocked":` + value + `}'`, "outputSchema": map[string]any{"type": "object"}}
			raw, _ := json.Marshal(map[string]any{"request": request})
			invokeCompact(t, r, "bot_event", string(raw))
			invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"after","id":"check","event":"event:desktop","seconds":1,"when":"event.unlocked == true"}}`)
			*now = now.Add(time.Second)
			waitOccurrence(t, r)
			want := 0
			if value == "true" {
				want = 1
			}
			if len(f.submissions) != want {
				t.Fatal("explicit CEL was bypassed")
			}
		})
	}
}

func TestNativeEventCarePurposeStillWaitsForPresence(t *testing.T) {
	r, f, _ := fixture(t)
	yes := true
	sample := care.Sample{}
	if err := r.ConfigureCare(func() care.Sample { return sample }); err != nil {
		t.Fatal(err)
	}
	invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"save","id":"care","label":"Care","prompt":"Offer a break","trigger":{"type":"event","purpose":"care","sources":["clock.minute"],"condition":"true","timeZone":"UTC"}}}`)
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.submissions) != 0 {
		t.Fatal("care ignored unknown presence")
	}
	sample.Presence = care.Presence{Awake: true, Unlocked: &yes}
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.submissions) != 1 {
		t.Fatal("care did not resume")
	}
}

func TestScheduleCapturesTrustedOriginalRequestsAndKeepsThemOnRetry(t *testing.T) {
	r, f, now := fixture(t)
	f.view.RuntimeOwner = "owner"
	f.view.InputSources = []api.InputSource{{RuntimeOwner: "owner", TurnKey: "original", RequestID: "weixin-input"}}
	ctx := api.WithToolInvocation(context.Background(), api.ToolInvocation{Provider: "codex", CallID: "call", Session: "session", Turn: "native", TurnKey: "original"})
	raw := json.RawMessage(`{"request":{"type":"after","id":"followup","seconds":1,"context":"Return result"}}`)
	if out := r.CallTool(ctx, "bot_schedule_update", raw); out.IsError {
		t.Fatal(out)
	}
	f.view.InputSources = []api.InputSource{{RuntimeOwner: "owner", TurnKey: "original", RequestID: "desktop-later"}}
	if out := r.CallTool(ctx, "bot_schedule_update", raw); out.IsError {
		t.Fatal(out)
	}
	// Reload the independent event ledger; a later registration retry cannot re-route it.
	r.initEvents()
	*now = now.Add(time.Second)
	waitOccurrence(t, r)
	if len(f.submissions) != 1 || len(f.submissions[0].OriginRequests) != 1 || f.submissions[0].OriginRequests[0] != "weixin-input" {
		t.Fatal("original route lost", f.submissions)
	}
}

func TestCalendarBatchDoesNotBorrowPhoneOriginAndPendingOriginIsImmutable(t *testing.T) {
	r, f, now := fixture(t)
	a := saveReminder(t, r, "phone-a")
	a.OriginRequests = []string{"phone-original"}
	r.state.Schedules[0] = a
	b := saveReminder(t, r, "phone-b")
	b.OriginRequests = []string{"phone-original"}
	r.state.Schedules[1] = b
	saveReminder(t, r, "legacy-unknown")
	*now = now.Add(time.Minute)
	f.view.CanSend = false
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(r.state.Wake.ScheduleIDs) != 2 {
		t.Fatal("unowned work was batched with phone task")
	}
	// Editing a future schedule must not reattribute its already-queued occurrence.
	b.Prompt = "changed future task"
	b.OriginRequests = []string{"desktop-later"}
	if _, err := r.Upsert(b); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove(a.ID); err != nil {
		t.Fatal(err)
	}
	f.view.CanSend = true
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.submissions) != 1 || len(f.submissions[0].OriginRequests) != 1 || f.submissions[0].OriginRequests[0] != "phone-original" {
		t.Fatal("pending source changed")
	}
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.submissions) != 2 || len(f.submissions[1].OriginRequests) != 0 {
		t.Fatal("legacy reminder borrowed phone route")
	}
}

func TestScheduleOriginRejectsUnscopedAndIncompleteEvidence(t *testing.T) {
	for _, scenario := range []string{"valid", "mixed", "wrong_owner", "missing_source", "wrong_provider", "unscoped_call"} {
		t.Run(scenario, func(t *testing.T) {
			r, f, _ := fixture(t)
			call := api.ToolInvocation{Provider: "codex", CallID: "call", Session: "session", Turn: "native", TurnKey: "turn"}
			f.view.RuntimeOwner = "owner"
			f.view.InputSources = []api.InputSource{{RuntimeOwner: "owner", TurnKey: "turn", RequestID: "phone"}}
			want := 1
			switch scenario {
			case "mixed":
				f.view.InputSources = append(f.view.InputSources, api.InputSource{RuntimeOwner: "owner", TurnKey: "turn", RequestID: "desktop"})
				want = 2
			case "wrong_owner":
				f.view.InputSources[0].RuntimeOwner = "other"
				want = 0
			case "missing_source":
				f.view.Items = []api.Item{{Kind: "user", TurnKey: "turn", RequestID: "unproven-steer"}}
				want = 0
			case "wrong_provider":
				call.Provider = "caelis"
				want = 0
			case "unscoped_call":
				call.TurnKey = ""
				want = 0
			}
			got := r.scheduleOrigin(api.WithToolInvocation(t.Context(), call))
			if len(got) != want {
				t.Fatal(got)
			}
		})
	}
}
