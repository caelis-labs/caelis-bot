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
	// Replacing the Runtime inside the same Service does not replace its epoch.
	s.setEngine(snapshotEngine{value: api.Snapshot{Revision: 1}})
	if got := s.Snapshot().ProcessEpoch; got != epoch {
		t.Fatalf("runtime replacement changed service epoch to %q", got)
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
