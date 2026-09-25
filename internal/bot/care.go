package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/care"
)

func (r *Runtime) ConfigureCare(sample func() care.Sample, extra ...care.Source) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil || r.stopped {
		return errors.New("care must be configured before start")
	}
	if sample == nil {
		return nil
	}
	e, err := care.OpenWithSources(filepath.Join(filepath.Dir(r.path), "care-"+r.provider+".json"), append(care.NativeSources(), extra...))
	if err != nil {
		return err
	}
	r.care, r.careSample = e, sample
	return nil
}
func (r *Runtime) tickCare(ctx context.Context) error {
	if r.care == nil {
		return nil
	}
	state := r.care.Snapshot()
	if len(state.Rules) == 0 && len(state.Activations) == 0 {
		return nil
	}
	now := r.now()
	sample := r.careSample()
	for _, event := range r.careSources.Poll(now, sample) {
		if err := r.care.Receive(ctx, event, now); err != nil {
			return err
		}
	}
	return r.care.Deliver(ctx, now, sample.Presence, r.engine.Snapshot().CanSend, func(id string) api.Receipt {
		if p, ok := r.engine.(api.BackgroundReceiptProvider); ok {
			return p.BackgroundReceipt(id)
		}
		return r.engine.Snapshot().LastReceipt
	}, func(ctx context.Context, a care.Activation) (api.Receipt, error) {
		// Re-sample immediately before submitting; time and event data cannot prove presence.
		if !r.careSample().Available() {
			return api.Receipt{ID: a.ID, Outcome: "rejected"}, nil
		}
		in := api.Submission{ID: a.ID, Text: carePrompt(a.Prompt), Scheduled: true}
		if b, ok := r.engine.(api.BackgroundRuntime); ok {
			return b.SubmitBackground(ctx, in, []string{care.GrantID(a.RuleID)})
		}
		return r.engine.Submit(ctx, in, nil)
	})
}
func carePrompt(prompt string) string {
	return "A registered proactive-care condition matched. Follow the standing assignment below. Be brief and timely. If no useful action or notification is warranted, return exactly " + api.SilentReminder + ".\n" + prompt
}
func (r *Runtime) callCare(ctx context.Context, args json.RawMessage) api.ToolResult {
	if r.care == nil {
		return result(nil, errors.New("native care observations unavailable"))
	}
	var in struct {
		Operation string `json:"operation"`
		care.Rule
		Event map[string]any `json:"event"`
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	if len(args) > 32768 || decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF {
		return result(nil, errors.New("invalid care arguments"))
	}
	switch in.Operation {
	case "list":
		return result(map[string]any{"sources": r.care.Sources(), "state": r.care.Snapshot(), "status": r.care.Status(), "presenceAvailable": r.careSample().Available(), "minimumGapSeconds": 300, "maximumActivationsPer24Hours": 8}, nil)
	case "test":
		matched, err := r.care.Test(ctx, in.Rule, in.Event, r.now())
		return result(map[string]bool{"matches": matched}, err)
	case "save":
		var authorize func(context.Context, string, string) error
		if b, ok := r.engine.(api.BackgroundRuntime); ok {
			authorize = b.AuthorizeBackground
		}
		rule, err := r.care.Save(ctx, in.Rule, authorize)
		return result(rule, err)
	case "remove":
		var revoke func(context.Context, string) error
		if b, ok := r.engine.(api.BackgroundRuntime); ok {
			revoke = b.RevokeBackground
		}
		return result("removed", r.care.Remove(ctx, in.ID, revoke))
	}
	return result(nil, errors.New("unsupported care operation"))
}
func careSpec() any {
	str := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	return map[string]any{"name": "bot_care", "description": "List, test, save or remove standing proactive-care rules. Conditions use bounded, pure CEL; only a matching condition queues the prompt for the resident Bot. Native sources: clock.minute, desktop.usage, desktop.appChanged. App must remain running; dispatch waits for known unlocked presence and an idle Bot. Conditions cannot execute commands or publish events. Discover registered adapters with list; do not assume that an installed CLI or connector is already an event source. Read the proactive-care skill guide for fields, examples and limits. A save must implement the user's request or existing standing arrangement; Caelis requires a user-originated registration grant.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"operation"}, "properties": map[string]any{
		"operation": map[string]any{"type": "string", "enum": []string{"list", "test", "save", "remove"}}, "id": str("Stable rule identifier, 1–64 letters, digits, hyphen or underscore"), "label": str("Brief user-facing purpose"), "on": map[string]any{"type": "string", "description": "Exact source name returned by list; only host-registered sources are accepted"}, "when": str("CEL boolean using event, now (timestamp), local (year, month, day, weekday 1–7, hour, minute)"), "prompt": str("Standing task, up to 4096 bytes"), "timeZone": str("Explicit IANA zone, e.g. Asia/Shanghai"), "cooldownSeconds": map[string]any{"type": "integer", "minimum": 60, "maximum": 31622400, "description": "Default 3600; cooldown starts when a match is queued"}, "expiresSeconds": map[string]any{"type": "integer", "minimum": 60, "maximum": 86400, "description": "Default 3600; queued work expires rather than catching up indefinitely"}, "event": map[string]any{"type": "object", "additionalProperties": true, "description": "Sample data for test only; never publishes an event or changes presence"},
	}}}
}

// PublishCareEvent is a host-only adapter entrypoint. It is intentionally absent
// from MCP, desktop services and the renderer bridge.
func (r *Runtime) PublishCareEvent(ctx context.Context, event care.Event) error {
	r.mu.Lock()
	stopped := r.stopped
	r.mu.Unlock()
	if stopped || r.care == nil {
		return errors.New("care is unavailable")
	}
	return r.care.Receive(ctx, event, r.now())
}
