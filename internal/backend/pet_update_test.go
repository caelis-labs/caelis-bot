package backend

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type recentRevisionEngine struct {
	revisionEngine
	recentCalls int
}

func (e *recentRevisionEngine) RecentSnapshot() api.Snapshot {
	e.recentCalls++
	v := e.value
	v.Items = api.RecentItems(v.Items, v.CurrentTurn)
	return v
}

func TestPetUpdateSkipsUnchangedProjectionAndObservesNativeTransitions(t *testing.T) {
	e := &recentRevisionEngine{revisionEngine: revisionEngine{revision: 7, snapshotEngine: snapshotEngine{value: api.Snapshot{Revision: 7, Phase: "working", CurrentTurn: "turn", Items: []api.Item{{Kind: "assistant", Text: "reply", TurnKey: "turn"}}}}}}
	s := NewService(e, nil, nil, nil, nil)
	s.ConfigureChat(filepath.Join(t.TempDir(), "chat.sqlite"))
	defer s.chat.Close()
	first := s.PetSnapshotUpdate(0, "", "").Snapshot
	if next := s.PetSnapshotUpdate(first.Revision, first.BotStatus, first.ProcessEpoch); next.Changed || e.recentCalls != 1 || e.calls != 0 {
		t.Fatal("unchanged pet poll projected history", next, e.recentCalls, e.calls)
	}
	for _, phase := range []string{"waiting_approval", "interrupted", "completed", "unknown"} {
		e.revision++
		e.value.Revision++
		e.value.Phase = phase
		e.value.LastReceipt = api.Receipt{ID: "original", Outcome: phase}
		e.value.Approvals = []api.Approval{{ID: "original-approval", Status: "pending"}}
		next := s.PetSnapshotUpdate(first.Revision, first.BotStatus, first.ProcessEpoch)
		if !next.Changed || next.Snapshot.Phase != phase || next.Snapshot.LastReceipt.ID != "original" || len(next.Snapshot.Approvals) != 1 {
			t.Fatal("native transition suppressed", phase, next)
		}
		first = next.Snapshot
	}
	s.SetBotStatus(func() string { return "scheduled work" })
	next := s.PetSnapshotUpdate(first.Revision, first.BotStatus, first.ProcessEpoch)
	if !next.Changed || next.Snapshot.BotStatus != "scheduled work" {
		t.Fatal("non-revision status hidden")
	}
	first = next.Snapshot
	s.RequireSetup(true)
	next = s.PetSnapshotUpdate(first.Revision, first.BotStatus, first.ProcessEpoch)
	if !next.Changed || next.Snapshot.Revision == first.Revision {
		t.Fatal("local revision hidden")
	}
	old := next.Snapshot
	s.BindRuntime(RuntimeBinding{Engine: &recentRevisionEngine{revisionEngine: revisionEngine{revision: 1, snapshotEngine: snapshotEngine{value: api.Snapshot{Revision: 1}}}}})
	next = s.PetSnapshotUpdate(old.Revision, old.BotStatus, old.ProcessEpoch)
	if !next.Changed || next.Snapshot.ProcessEpoch == old.ProcessEpoch {
		t.Fatal("replacement epoch suppressed")
	}
}

func TestPetUpdateStillReconcilesOriginalDraftReceipt(t *testing.T) {
	e := &recentRevisionEngine{revisionEngine: revisionEngine{revision: 7, snapshotEngine: snapshotEngine{value: api.Snapshot{
		Revision: 7, CurrentTurn: "later", LastReceipt: api.Receipt{ID: "different", Outcome: "accepted"},
		Items: []api.Item{{Kind: "user", RequestID: "original", TurnKey: "old"}, {Kind: "user", RequestID: "different", TurnKey: "later"}},
	}}}}
	s := NewService(e, nil, nil, nil, nil)
	first := s.PetSnapshot()
	s.draftSend = &draftSend{ID: "original"}
	s.PetSnapshotUpdate(first.Revision, first.BotStatus, first.ProcessEpoch)
	if e.calls != 1 || s.draftSend == nil || s.draftSend.Outcome != "accepted" {
		t.Fatal("original accepted evidence outside the preview boundary was not reconciled", e.calls, s.draftSend)
	}
}

func TestPetPreviewRetainsCompletedIMWhenNativeHistoryIsEmpty(t *testing.T) {
	e := &recentRevisionEngine{revisionEngine: revisionEngine{revision: 7, snapshotEngine: snapshotEngine{value: api.Snapshot{Revision: 7, Connection: "offline"}}}}
	s := NewService(e, nil, nil, nil, nil)
	s.ConfigureChat(filepath.Join(t.TempDir(), "chat.sqlite"))
	defer s.chat.Close()
	s.chat.Observe([]api.Item{{ID: "saved-answer", Kind: "assistant", Text: "completed answer", Status: "completed", TurnKey: "saved-turn"}})
	first := s.PetSnapshotUpdate(0, "", "").Snapshot
	if len(first.Items) != 1 || first.Items[0].ID != "saved-answer" {
		t.Fatal("native reconnect erased completed IM preview", first)
	}
	if s.PetSnapshotUpdate(first.Revision, first.BotStatus, first.ProcessEpoch).Changed {
		t.Fatal("unchanged saved preview rebuilt")
	}
	s.chat.Observe([]api.Item{{ID: "new-answer", Kind: "assistant", Text: "new saved answer", Status: "completed", TurnKey: "saved-turn"}})
	next := s.PetSnapshotUpdate(first.Revision, first.BotStatus, first.ProcessEpoch)
	if !next.Changed || len(next.Snapshot.Items) != 1 || next.Snapshot.Items[0].ID != "new-answer" || next.Snapshot.Revision < first.Revision {
		t.Fatal("IM revision did not refresh the retained preview", next)
	}
}

func BenchmarkPetSnapshotPolling(b *testing.B) {
	e := &recentRevisionEngine{revisionEngine: revisionEngine{revision: 7, snapshotEngine: snapshotEngine{value: api.Snapshot{Revision: 7, Items: []api.Item{{Kind: "assistant", Text: strings.Repeat("synthetic ", 1000)}}}}}}
	s := NewService(e, nil, nil, nil, nil)
	first := s.PetSnapshot()
	b.Run("full_projection", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.PetSnapshot()
		}
	})
	b.Run("unchanged_revision", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.PetSnapshotUpdate(first.Revision, first.BotStatus, first.ProcessEpoch)
		}
	})
}
