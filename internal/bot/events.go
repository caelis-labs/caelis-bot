package bot

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/eventplan"
)

// ShellObserver executes registered commands through the managed local Core.
type ShellObserver interface {
	ShellObservationAvailable(context.Context) bool
	ObserveShell(context.Context, string) (json.RawMessage, error)
}

func (r *Runtime) ConfigureShellObserver(observer ShellObserver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shellObserver = observer
}

func (r *Runtime) shellObservationAvailable(ctx context.Context) bool {
	r.mu.Lock()
	observer, engine := r.shellObserver, r.engine
	r.mu.Unlock()
	if p, ok := engine.(api.ApplicationCapabilityProvider); ok && !p.ApplicationCapabilities().ScheduledActivation {
		return false
	}
	return observer != nil && observer.ShellObservationAvailable(ctx)
}

type shellUnavailable struct{}

func (shellUnavailable) Error() string          { return "Notebook Shell observation is unavailable" }
func (shellUnavailable) ConfirmedFailure() bool { return true }

func (r *Runtime) initEvents() {
	r.events, r.eventsErr = eventplan.Open(filepath.Join(filepath.Dir(r.path), "events-"+r.provider+".json"), r.runEvent, r.now())
	if r.eventsErr == nil {
		r.migrateNativeEvents()
	}
}

func (r *Runtime) migrateNativeEvents() error {
	err := r.events.MigrateLegacy(filepath.Join(filepath.Dir(r.path), "care-"+r.provider+".json"), r.now())
	r.mu.Lock()
	r.nativeErr = err
	r.mu.Unlock()
	return err
}

func (r *Runtime) runEvent(ctx context.Context, v eventplan.Event) (json.RawMessage, error) {
	r.mu.Lock()
	shell := r.shellObserver
	r.mu.Unlock()
	if shell == nil {
		return nil, shellUnavailable{}
	}
	return shell.ObserveShell(ctx, v.Command)
}

func (r *Runtime) eventError() error {
	if r.eventsErr != nil || r.events == nil {
		return errors.New("event store unavailable; existing data preserved")
	}
	return nil
}

func (r *Runtime) eventSchedule(ctx context.Context, name string, q map[string]any) (api.ToolResult, bool) {
	kind, handle := str(q, "type"), str(q, "automation")
	managed := name == "bot_schedule" && (kind == "events" || kind == "read") ||
		name == "bot_schedule_update" && (kind == "after" || kind == "at" || kind == "interval" ||
			kind == "remove" && (strings.HasPrefix(handle, "schedule:") || strings.HasPrefix(handle, "event:")))
	if !managed {
		return api.ToolResult{}, false
	}
	fail := func(err error) (api.ToolResult, bool) { return compactError("event_arrangement", err), true }
	if err := r.eventError(); err != nil {
		return fail(err)
	}
	if name == "bot_schedule" {
		s := r.events.Snapshot()
		if kind == "events" {
			items := []map[string]any{}
			for _, v := range s.Events {
				items = append(items, map[string]any{"event": "event:" + v.ID, "label": v.Label, "kind": v.Kind, "retain": v.Retain})
			}
			return summaryPage(items, q, "event"), true
		}
		h := str(q, "handle")
		for _, v := range s.Events {
			if h == "event:"+v.ID {
				return boundedData(map[string]any{"ok": true, "data": v}, q), true
			}
		}
		for _, p := range s.Plans {
			if h == "schedule:"+p.ID {
				p.OriginRequests, p.GrantID, p.Fingerprint = nil, "", ""
				return boundedData(map[string]any{"ok": true, "data": p, "latestOccurrence": latestOccurrence(s, p.ID)}, q), true
			}
		}
		for _, o := range s.Occurrences {
			if h == o.ID {
				o.OriginRequests = nil
				return boundedData(map[string]any{"ok": true, "data": o}, q), true
			}
		}
		return fail(errors.New("handle unavailable or history retention elapsed"))
	}

	if kind == "remove" {
		domain, id, _ := strings.Cut(handle, ":")
		var err error
		if domain == "event" {
			err = r.events.RemoveEvent(id)
		} else {
			err = r.events.Remove(id, r.now())
		}
		if err != nil {
			return fail(err)
		}
		return compactValue(map[string]any{"ok": true}, false), true
	}

	raw, _ := json.Marshal(q)
	var p eventplan.Plan
	if err := json.Unmarshal(raw, &p); err != nil {
		return fail(errors.New("invalid arrangement arguments"))
	}
	p.Kind, p.OriginRequests = kind, r.scheduleOrigin(ctx)
	label := str(q, "label")
	if label == "" {
		label = p.ID
	}
	var definition *eventplan.Event
	retain, _ := q["retain"].(bool)
	if p.Event != "" {
		handle, ok := strings.CutPrefix(p.Event, "event:")
		if !ok || str(q, "observe") != "" || retain || str(q, "context") != "" || str(q, "label") != "" {
			return fail(errors.New("reuse an event by exact handle with timing and condition, or supply an inline definition"))
		}
		p.Event = handle
	} else {
		v := eventplan.Event{ID: p.ID, Label: label, Context: str(q, "context"), Kind: "wake", Retain: retain}
		if command := str(q, "observe"); command != "" {
			if strings.TrimSpace(p.When) == "" {
				return fail(errors.New("Shell observation requires a wake condition"))
			}
			v.Kind, v.Command = "shell", command
		} else if retain {
			return fail(errors.New("retain applies to Notebook observation definitions"))
		}
		definition = &v
	}
	saved, err := r.events.Save(p, definition, r.now(), func() bool { return r.shellObservationAvailable(ctx) })
	if errors.Is(err, eventplan.ErrObservationUnavailable) {
		return compactError("shell_observation_unavailable", shellUnavailable{}), true
	}
	if err != nil {
		return fail(err)
	}
	receipt := map[string]any{"ok": true, "automation": "schedule:" + saved.ID, "event": "event:" + saved.Event, "status": saved.Status}
	if saved.Finished.IsZero() {
		receipt["next"] = saved.Next
	}
	return compactValue(receipt, false), true
}

func (r *Runtime) tickEvents(ctx context.Context) error {
	if r.events == nil {
		return nil
	}
	r.mu.Lock()
	nativeErr := r.nativeErr
	r.mu.Unlock()
	if errors.Is(nativeErr, eventplan.ErrWritePending) {
		if err := r.migrateNativeEvents(); errors.Is(err, eventplan.ErrWritePending) {
			return err
		}
	}
	return r.events.Tick(ctx, r.now(), eventplan.Delivery{
		Ready: func() bool { return r.engine.Snapshot().CanSend },
		Revoke: func(id string) error {
			if b, ok := r.engine.(api.BackgroundGrantRevoker); ok {
				return b.RevokeBackground(ctx, id)
			}
			return nil
		},
		Submit: func(ctx context.Context, wake eventplan.Wake) (string, error) {
			receipt, err := r.submitSystemLocked(ctx, api.SystemMessage{ID: wake.ID, Text: wake.Text, Source: api.SystemCallback, OriginRequests: wake.OriginRequests, Grants: []string{wake.Grant}})
			if receipt.ID != wake.ID {
				return "unknown", errors.New("wake receipt identity mismatch")
			}
			if receipt.Outcome == "rejected" && receipt.Deferred {
				return "deferred", err
			}
			return receipt.Outcome, err
		},
		Receipt: func(id string) string {
			receipt := r.SystemReceipt(id)
			if receipt.ID != id {
				return "unknown"
			}
			return receipt.Outcome
		},
		Settled: r.forgetCallbackDeliveries,
	})
}

func latestOccurrence(s eventplan.State, plan string) string {
	for i := len(s.Occurrences) - 1; i >= 0; i-- {
		if s.Occurrences[i].Plan == plan {
			return s.Occurrences[i].ID
		}
	}
	return ""
}
