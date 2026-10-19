package bot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/eventplan"
)

func migratedScheduleFixture(t *testing.T, r *Runtime, now time.Time, oldStatus string) (eventplan.Plan, eventplan.Plan) {
	t.Helper()
	dir := t.TempDir()
	old := map[string]any{
		"version": 2, "policy": eventplan.DefaultPolicy(), "watermarks": map[string]time.Time{},
		"rules": []map[string]any{{"id": "work-break", "label": "Work break", "on": "clock.minute", "when": "true", "prompt": "Report the requested result", "timeZone": "UTC", "version": "current", "enabled": true, "cooldownSeconds": 60, "expiresSeconds": 900}},
		"activations": []map[string]any{
			{"id": "original-pending", "ruleID": "work-break", "version": "current", "status": "pending", "expires": now.Add(15 * time.Minute), "originRequests": []string{"original-phone-input"}},
			{"id": "original-history", "ruleID": "work-break", "version": "previous", "status": oldStatus, "prompt": "Previous requested result"},
		},
	}
	path := filepath.Join(dir, "care-fixture.json")
	if err := os.WriteFile(path, encoded(old), 0600); err != nil {
		t.Fatal(err)
	}
	e, err := eventplan.Open(filepath.Join(filepath.Dir(r.path), "migrated-events.json"), nil, now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	if err = e.MigrateLegacy(path, now); err != nil {
		t.Fatal(err)
	}
	r.events.Close()
	r.events = e
	plans := e.Snapshot().Plans
	return plans[0], plans[1]
}

type referenceGrantEngine struct {
	*scheduleGrantEngine
	complete map[string]bool
	revokes  int
}

func (f *referenceGrantEngine) RevokeBackground(_ context.Context, id string) error {
	if id == f.authorized {
		f.revokes++
		f.revoked = id
	}
	return nil
}
func (f *referenceGrantEngine) SubmitBackground(ctx context.Context, in api.Submission, ids []string) (api.Receipt, error) {
	if f.revoked != "" {
		return api.Receipt{ID: in.ID, Outcome: "rejected"}, errors.New("original grant revoked")
	}
	return f.scheduleGrantEngine.SubmitBackground(ctx, in, ids)
}
func (f *referenceGrantEngine) BackgroundResult(id string) api.BackgroundResult {
	return api.BackgroundResult{ID: id, Complete: f.complete[id]}
}

func TestRemovingMigratedHistoryPreservesActiveAndUnresolvedGrant(t *testing.T) {
	for _, oldStatus := range []string{"expired", "unknown", "accepted"} {
		t.Run(oldStatus, func(t *testing.T) {
			r, base, now := fixture(t)
			active, history := migratedScheduleFixture(t, r, *now, oldStatus)
			f := &referenceGrantEngine{scheduleGrantEngine: &scheduleGrantEngine{fakeEngine: base, authorized: active.Authorization()}, complete: map[string]bool{}}
			r.engine = f
			remove := func(id string) {
				invokeCompact(t, r, "bot_schedule_update", string(encoded(map[string]any{"request": map[string]any{"type": "remove", "automation": "schedule:" + id}})))
			}
			remove(history.ID)
			if f.revokes != 0 {
				t.Error("removing historical Plan revoked the live shared grant")
			}
			// Restart between cancellation and retirement; do not rely on an
			// in-memory reference count or issue a replacement activation.
			r.events.Close()
			restored, err := eventplan.Open(filepath.Join(filepath.Dir(r.path), "migrated-events.json"), nil, *now)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restored.Close)
			r.events = restored
			if err := r.tickEvents(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(base.submissions) != 1 || base.submissions[0].ID != "original-pending" || len(base.submissions[0].OriginRequests) != 1 || base.submissions[0].OriginRequests[0] != "original-phone-input" {
				t.Fatal("active migrated schedule could not dispatch under its original grant and origin", base.submissions)
			}
			remove(active.ID)
			if err := r.tickEvents(t.Context()); err != nil {
				t.Fatal(err)
			}
			if f.revokes != 0 {
				t.Fatal("accepted or unknown receipt lost its grant")
			}
			f.complete["original-pending"] = true
			if err := r.tickEvents(t.Context()); err != nil {
				t.Fatal(err)
			}
			if oldStatus != "expired" && f.revokes != 0 {
				t.Fatal("shared historical receipt was still unresolved")
			}
			f.complete["original-history"] = true
			*now = now.Add(5 * time.Second)
			for range 2 {
				if err := r.tickEvents(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if f.revokes != 1 || len(base.submissions) != 1 {
				t.Fatal("last receipt did not retire exactly once or work replayed", f.revokes, len(base.submissions))
			}
		})
	}
}

func TestScheduleListNativeIdentityLabelAndPagination(t *testing.T) {
	for _, migrated := range []bool{false, true} {
		name := "new"
		if migrated {
			name = "migrated"
		}
		t.Run(name, func(t *testing.T) {
			r, _, now := fixture(t)
			if migrated {
				migratedScheduleFixture(t, r, *now, "expired")
			}
			for _, id := range []string{"work-break", "water-break"} {
				if migrated && id == "work-break" {
					continue
				}
				label := "Work break"
				if id == "water-break" {
					label = "Water break"
				}
				invokeCompact(t, r, "bot_schedule_update", string(encoded(map[string]any{"request": map[string]any{"type": "save", "id": id, "label": label, "prompt": "Requested result", "trigger": map[string]any{"type": "event", "sources": []string{"clock.minute"}, "condition": "true", "timeZone": "UTC"}}})))
			}
			saveReminder(t, r, "work-break")
			list := func(query, kind, cursor string) api.ToolResult {
				return r.CallTool(t.Context(), "bot_schedule", encoded(map[string]any{"request": map[string]any{"type": "list", "query": query, "kind": kind, "limit": 1, "cursor": cursor}}))
			}
			data := func(out api.ToolResult) map[string]any {
				t.Helper()
				if out.IsError {
					t.Fatal(out)
				}
				return out.StructuredContent["data"].(map[string]any)
			}
			p, _ := r.events.NativePlan("work-break")
			for _, query := range []string{"work-break", "WORK BREAK", p.ID, p.Event} {
				d := data(list(query, "event", ""))
				items := d["items"].([]map[string]any)
				if len(items) != 1 || d["total"] != 1 {
					t.Errorf("query %q did not find one native arrangement: %v", query, d)
					continue
				}
				if items[0]["label"] != "Work break" || items[0]["nativeId"] != "work-break" || items[0]["automation"] != "schedule:"+p.ID {
					t.Error("list lost recognizable identity or stable handle", items[0])
				}
			}
			first := data(list("break", "event", ""))
			cursor, ok := first["nextCursor"].(string)
			if !ok || first["total"] != 2 {
				t.Fatal("native query did not paginate", first)
			}
			second := data(list("BREAK", "event", cursor))
			a, b := first["items"].([]map[string]any), second["items"].([]map[string]any)
			if len(a) != 1 || len(b) != 1 || a[0]["automation"] == b[0]["automation"] || second["nextCursor"] != nil {
				t.Fatal("pagination duplicated or omitted an arrangement", first, second)
			}
			if !list("work-break", "event", cursor).IsError || !list("break", "calendar", cursor).IsError {
				t.Fatal("cursor survived changed query or kind")
			}
			if d := data(list("work-break", "calendar", "")); d["total"] != 1 {
				t.Fatal("calendar filter included native arrangements", d)
			}
			invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"remove","automation":"event:work-break"}}`)
			if d := data(list("work-break", "event", "")); d["total"] != 0 || !list("break", "event", cursor).IsError {
				t.Fatal("cancelled arrangement still listed or old cursor accepted", d)
			}
		})
	}
}
