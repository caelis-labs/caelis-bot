package bot

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/eventplan"
	"github.com/caelis-labs/caelis-bot/internal/scheduledata"
)

func (r *Runtime) ConfigureNativeSources(sample func() scheduledata.Sample) error {
	if !r.step.TryLock() {
		return errors.New("native observations waiting for current operation")
	}
	defer r.step.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return errors.New("Bot stopped")
	}
	r.nativeSample = sample
	return r.nativeErr
}
func (r *Runtime) saveNativeSchedule(ctx context.Context, q map[string]any) api.ToolResult {
	if err := r.eventError(); err != nil {
		return compactError("events_unavailable", err)
	}
	if r.nativeErr != nil {
		return compactError("native_arrangements_unavailable", errors.New("saved native arrangements need recovery; original data preserved"))
	}
	trigger := q["trigger"].(map[string]any)
	n := eventplan.NativeArrangement{ID: str(q, "id"), Label: str(q, "label"), Prompt: str(q, "prompt"), When: str(trigger, "condition"), TimeZone: str(trigger, "timeZone"), OriginRequests: r.scheduleOrigin(ctx)}
	raw, _ := json.Marshal(trigger["sources"])
	_ = json.Unmarshal(raw, &n.Sources)
	raw, _ = json.Marshal(trigger)
	var timing struct{ CooldownSeconds, ExpiresSeconds int }
	_ = json.Unmarshal(raw, &timing)
	n.CooldownSeconds = timing.CooldownSeconds
	n.ExpiresSeconds = timing.ExpiresSeconds
	p, err := r.events.SaveNative(n, r.now(), func(p eventplan.Plan) error { return r.authorizeEvent(ctx, p.Authorization(), n) })
	if err != nil {
		return compactError("native_arrangement", err)
	}
	return compactValue(map[string]any{"ok": true, "automation": "event:" + n.ID, "schedule": "schedule:" + p.ID, "status": p.Status}, false)
}
func (r *Runtime) configureSchedulePolicy(ctx context.Context, q map[string]any) api.ToolResult {
	if err := r.eventError(); err != nil {
		return compactError("events_unavailable", err)
	}
	var p eventplan.Policy
	raw, _ := json.Marshal(q["policy"])
	_ = json.Unmarshal(raw, &p)
	err := r.events.ConfigurePolicy(p, func() error { return r.authorizeEvent(ctx, "schedule-policy:limits", p) })
	if err != nil {
		return compactError("schedule_policy", err)
	}
	return compactValue(map[string]any{"ok": true, "data": r.events.Snapshot().Policy}, false)
}
