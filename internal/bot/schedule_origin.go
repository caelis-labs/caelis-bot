package bot

import (
	"context"
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Capture only the authenticated tool turn's native sources. Neither tool JSON,
// transcript text, latest phone input nor a similar turn ID can supply a route.
// Keep all sources so a mixed desktop/phone turn cannot acquire phone ownership.
func (r *Runtime) scheduleOrigin(ctx context.Context) []string {
	call, ok := api.ToolInvocationFromContext(ctx)
	if !ok || call.Provider != r.provider || call.TurnKey == "" || r.engine == nil {
		return nil
	}
	snapshot := r.engine.Snapshot()
	if snapshot.RuntimeOwner == "" {
		return nil
	}
	var requests []string
	sources := append([]api.InputSource(nil), snapshot.InputSources...)
	for _, source := range snapshot.ScheduledSources {
		sources = append(sources, api.InputSource{RuntimeOwner: source.RuntimeOwner, TurnKey: source.TurnKey, RequestID: source.RequestID})
	}
	for _, source := range sources {
		if source.TurnKey != call.TurnKey {
			continue
		}
		if source.RuntimeOwner != snapshot.RuntimeOwner || source.RequestID == "" {
			return nil
		}
		requests = append(requests, source.RequestID)
	}
	// Display user items may disqualify incomplete provenance, never establish it.
	for _, item := range snapshot.Items {
		if item.Kind == "user" && item.TurnKey == call.TurnKey && !slices.Contains(requests, item.RequestID) {
			return nil
		}
	}
	slices.Sort(requests)
	return slices.Compact(requests)
}
