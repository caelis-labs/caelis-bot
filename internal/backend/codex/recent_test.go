package codex

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRecentSnapshotDoesNotAliasPublishedItemsOrHydrateHistory(t *testing.T) {
	source := api.Snapshot{Revision: 7, CurrentTurn: "current", RuntimeOwner: "owner", LastReceipt: api.Receipt{ID: "original", Outcome: "unknown"},
		InputSources: []api.InputSource{{RuntimeOwner: "owner", TurnKey: "current", RequestID: "request"}},
		Items:        []api.Item{{Kind: "user", TurnKey: "old"}, {Kind: "activity", TurnKey: "current", Activity: &api.Activity{Kind: "read"}}, {Kind: "user", TurnKey: "current"}}}
	s := &Session{}
	s.cached.Store(&source)
	got := s.RecentSnapshot()
	if got.Revision != 7 || len(got.Items) != 2 || got.Items[0].Activity.Kind != "read" || got.LastReceipt != source.LastReceipt || got.RuntimeOwner != source.RuntimeOwner {
		t.Fatal("recent projection lost native facts", got)
	}
	got.Items[0].Activity.Kind = "mutated"
	got.InputSources[0].RequestID = "mutated"
	if source.Items[1].Activity.Kind != "read" || source.InputSources[0].RequestID != "request" {
		t.Fatal("preview aliased published state")
	}
}
