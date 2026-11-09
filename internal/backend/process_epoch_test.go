package backend

import (
	"fmt"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestProcessEpochIsStableAcrossServiceSnapshots(t *testing.T) {
	s := NewService(snapshotEngine{value: api.Snapshot{ProcessEpoch: "adapter-owned", Revision: 7}}, nil, nil, nil, nil)
	epoch := s.Snapshot().ProcessEpoch
	if epoch == "" || epoch == "adapter-owned" {
		t.Fatal("service did not stamp its own epoch")
	}
	for range 2 {
		for _, snapshot := range []api.Snapshot{s.Snapshot(), s.PetSnapshot(), s.ChatSnapshot(0, "", "").Snapshot} {
			if snapshot.ProcessEpoch != epoch {
				t.Fatalf("snapshot epoch = %q, want %q", snapshot.ProcessEpoch, epoch)
			}
		}
	}
	// Replacing the Runtime starts a new revision lifetime.
	s.setEngine(snapshotEngine{value: api.Snapshot{Revision: 1}})
	if got := s.Snapshot().ProcessEpoch; got == epoch || got == "" {
		t.Fatalf("runtime replacement reused service epoch %q", got)
	}
}

func TestReplacementServiceReturnsFullChatSnapshotAcrossEpochs(t *testing.T) {
	previous := NewService(snapshotEngine{value: api.Snapshot{Revision: 7}}, nil, nil, nil, nil).Snapshot()
	for _, revisionSource := range []bool{false, true} {
		for _, revision := range []uint64{7, 3} {
			t.Run(fmt.Sprintf("revisionSource=%v/revision=%d", revisionSource, revision), func(t *testing.T) {
				value := api.Snapshot{Revision: revision, Items: []api.Item{{ID: "replacement-reply", Kind: "assistant", Text: "current reply"}}}
				var engine api.Engine = snapshotEngine{value: value}
				if revisionSource {
					engine = &revisionEngine{snapshotEngine: snapshotEngine{value: value}, revision: revision}
				}
				s := NewService(engine, nil, nil, nil, nil)
				current := s.Snapshot()
				if current.ProcessEpoch == "" || current.ProcessEpoch == previous.ProcessEpoch {
					t.Fatal("replacement service reused the previous epoch")
				}
				for _, epoch := range []string{previous.ProcessEpoch, ""} {
					update := s.ChatSnapshot(previous.Revision, previous.BotStatus, epoch)
					if !update.Changed || update.Snapshot.ProcessEpoch != current.ProcessEpoch || update.Snapshot.Revision != revision || len(update.Snapshot.Items) != 1 || update.Snapshot.Items[0].ID != "replacement-reply" {
						t.Fatalf("replacement snapshot was suppressed or incomplete: %+v", update)
					}
				}
				if update := s.ChatSnapshot(current.Revision, current.BotStatus, current.ProcessEpoch); update.Changed {
					t.Fatal("matching epoch did not preserve unchanged polling")
				}
			})
		}
	}
}

func TestRuntimeBindingReturnsLowerRevisionInNewEpoch(t *testing.T) {
	s := NewService(snapshotEngine{value: api.Snapshot{Revision: 120}}, nil, nil, nil, nil)
	old := s.Snapshot()
	s.BindRuntime(RuntimeBinding{Engine: snapshotEngine{value: api.Snapshot{Revision: 8, Items: []api.Item{{ID: "new-reply", Kind: "assistant", Text: "new runtime"}}}}})
	update := s.ChatSnapshot(old.Revision, old.BotStatus, old.ProcessEpoch)
	if !update.Changed || update.Snapshot.ProcessEpoch == old.ProcessEpoch || update.Snapshot.Revision != 9 || len(update.Snapshot.Items) != 1 || update.Snapshot.Items[0].ID != "new-reply" {
		t.Fatalf("runtime replacement suppressed or mixed snapshot: %+v", update)
	}
}

type delayedObservationEngine struct {
	snapshotEngine
	entered chan struct{}
	release chan struct{}
}

func (e *delayedObservationEngine) Snapshot() api.Snapshot {
	close(e.entered)
	<-e.release
	return e.value
}
func (e *delayedObservationEngine) RecentSnapshot() api.Snapshot   { return e.Snapshot() }
func (e *delayedObservationEngine) ComposerSnapshot() api.Snapshot { return e.Snapshot() }
func (e *delayedObservationEngine) Revision() uint64 {
	close(e.entered)
	<-e.release
	return e.value.Revision
}

func TestRuntimeObservationCapturesEngineEpochAndRevisionTogether(t *testing.T) {
	for _, surface := range []string{"snapshot", "pet", "composer", "chat", "unchanged", "pet-unchanged"} {
		t.Run(surface, func(t *testing.T) {
			e := &delayedObservationEngine{snapshotEngine: snapshotEngine{value: api.Snapshot{Revision: 120}}, entered: make(chan struct{}), release: make(chan struct{})}
			s := NewService(e, nil, nil, nil, nil)
			oldEpoch := s.processEpoch
			result := make(chan api.Snapshot, 1)
			go func() {
				switch surface {
				case "pet":
					result <- s.PetSnapshot()
				case "composer":
					result <- s.ComposerSnapshot()
				case "chat":
					result <- s.ChatSnapshot(0, "", "").Snapshot
				case "pet-unchanged":
					result <- s.PetSnapshotUpdate(120, "", oldEpoch).Snapshot
				case "unchanged":
					result <- s.ChatSnapshot(120, "", oldEpoch).Snapshot
				default:
					result <- s.Snapshot()
				}
			}()
			<-e.entered
			s.BindRuntime(RuntimeBinding{Engine: snapshotEngine{value: api.Snapshot{Revision: 8}}})
			current := s.Snapshot()
			close(e.release)
			late := <-result
			if current.ProcessEpoch == oldEpoch || current.Revision != 9 {
				t.Fatalf("invalid current observation: %+v", current)
			}
			if surface == "unchanged" || surface == "pet-unchanged" {
				if late.ProcessEpoch != current.ProcessEpoch || late.Revision != current.Revision {
					t.Fatalf("stale revision check suppressed replacement: %+v", late)
				}
			} else if late.ProcessEpoch != oldEpoch || late.Revision != 120 {
				t.Fatalf("old engine was stamped with replacement epoch/revision: %+v", late)
			}
		})
	}
}
