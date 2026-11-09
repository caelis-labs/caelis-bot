package caelis

import (
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRecentSnapshotFiltersBeforeCloneAndRetainsPrivateReceipts(t *testing.T) {
	source := api.Snapshot{Revision: 9, CurrentTurn: "current", Phase: "unknown", RuntimeOwner: "owner",
		LastReceipt:  api.Receipt{ID: "original", Outcome: "unknown"},
		InputSources: []api.InputSource{{RuntimeOwner: "owner", TurnKey: "current", RequestID: "request"}},
		Approvals:    []api.Approval{{ID: "pending", NativeID: "native", TaskID: "task", Status: "pending"}},
		Items: []api.Item{
			{ID: "old", Kind: "user", TurnKey: "old", Details: strings.Repeat("synthetic", 10000)},
			{ID: "tool", Kind: "activity", TurnKey: "current", Activity: &api.Activity{Kind: "read"}},
			{ID: "steer", Kind: "user", TurnKey: "current"},
			{ID: "reply", Kind: "assistant", TurnKey: "current", Text: "answer"},
		}}
	s := &Session{}
	s.cached.Store(&source)
	got := s.RecentSnapshot()
	if got.Revision != 9 || got.LastReceipt != source.LastReceipt || got.Phase != "unknown" || len(got.Items) != 3 || got.Items[0].ID != "tool" || got.Approvals[0].NativeID != "native" || got.InputSources[0] != source.InputSources[0] {
		t.Fatal("preview changed native facts", got)
	}
	got.Items[0].Activity.Kind = "mutated"
	got.Approvals[0].Status = "resolved"
	got.InputSources[0].RequestID = "mutated"
	if source.Items[1].Activity.Kind != "read" || source.Approvals[0].Status != "pending" || source.InputSources[0].RequestID != "request" {
		t.Fatal("preview mutated the published snapshot")
	}
	source.Revision = 1
	source.CurrentTurn = "replacement"
	source.Items = []api.Item{{ID: "new", Kind: "assistant", TurnKey: "replacement"}}
	s.cached.Store(&source)
	if next := s.RecentSnapshot(); next.Revision != 1 || len(next.Items) != 1 || next.Items[0].ID != "new" {
		t.Fatal("revision replacement reused earlier projection")
	}
}

func BenchmarkRecentSnapshot(b *testing.B) {
	source := api.Snapshot{Revision: 9, CurrentTurn: "current"}
	for range 100 {
		source.Items = append(source.Items, api.Item{Kind: "assistant", TurnKey: "old", Details: strings.Repeat("synthetic ", 2000)})
	}
	source.Items = append(source.Items, api.Item{Kind: "user", TurnKey: "current", Text: "request"}, api.Item{Kind: "assistant", TurnKey: "current", Text: "answer"})
	s := &Session{}
	s.cached.Store(&source)
	s.mu.Lock() // Exercise the same published snapshot in both cases.
	defer s.mu.Unlock()
	b.Run("full_cached_snapshot", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.Snapshot()
		}
	})
	b.Run("filter_before_clone", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.RecentSnapshot()
		}
	})
}
