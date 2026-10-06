package backend

import (
	"context"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type delayedInput struct {
	api.Engine
	mu               sync.Mutex
	view             api.Snapshot
	entered, release chan struct{}
	outcome          string
}

func (e *delayedInput) Snapshot() api.Snapshot { e.mu.Lock(); defer e.mu.Unlock(); return e.view }
func (e *delayedInput) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	close(e.entered)
	<-e.release
	r := api.Receipt{ID: in.ID, Outcome: e.outcome}
	e.mu.Lock()
	e.view.LastReceipt = r
	e.mu.Unlock()
	return r, nil
}
func TestOutgoingVisibleBeforeReceiptAndReconcilesByNativeIdentity(t *testing.T) {
	for _, outcome := range []string{"accepted", "rejected", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			e := &delayedInput{entered: make(chan struct{}), release: make(chan struct{}), outcome: outcome, view: api.Snapshot{Revision: 1, Phase: "working", Items: []api.Item{{ID: "old", Kind: "user", Text: "same", RequestID: "earlier"}}}}
			s := NewService(e, func([]string) ([]api.InputFile, error) { return []api.InputFile{{Name: "attached.txt"}}, nil }, func([]string) {}, nil, nil)
			_, _ = s.SaveDraft(api.Draft{Text: "same"})
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = s.Submit(t.Context(), api.Submission{ID: "new-request", Text: "same"})
			}()
			<-e.entered
			v := s.ChatSnapshot(1, "").Snapshot
			if len(v.Items) != 2 || v.Items[1].RequestID != "new-request" || v.Items[1].Status != "sending" || v.Items[1].Text != "same\nattached.txt" {
				t.Fatalf("input missing before receipt: %+v", v.Items)
			}
			close(e.release)
			<-done
			v = s.Snapshot()
			if len(v.Items) != 2 || v.Items[1].Status != outcome {
				t.Fatalf("receipt lost: %+v", v.Items)
			}
			if outcome != "accepted" && s.Draft().Text != "same" {
				t.Fatal("unaccepted draft cleared")
			}
			// A late native input proves acceptance even after an unknown response.
			e.mu.Lock()
			e.view.Items = append(e.view.Items, api.Item{ID: "native", Kind: "user", RequestID: "new-request", Text: "same", Status: "completed"})
			e.mu.Unlock()
			v = s.Snapshot()
			if len(v.Items) != 2 || v.Items[1].ID != "native" || len(s.outbox) != 0 {
				t.Fatalf("native input duplicated: %+v", v.Items)
			}
		})
	}
}

func TestOutgoingKeepsSubmissionOrderDuringDelayedNativeMessages(t *testing.T) {
	e := &delayedInput{view: api.Snapshot{}}
	s := NewService(e, nil, nil, nil, nil)
	s.stageOutgoing(api.Submission{ID: "first", Text: "one"}, nil)
	s.stageOutgoing(api.Submission{ID: "second", Text: "two"}, nil)
	e.view.Items = []api.Item{{ID: "reply", Kind: "assistant", Text: "early output"}}
	v := s.Snapshot()
	if len(v.Items) != 3 || v.Items[0].RequestID != "first" || v.Items[1].RequestID != "second" {
		t.Fatalf("submission order lost: %+v", v.Items)
	}
	e.view.Items = []api.Item{{ID: "native-first", RequestID: "first", Kind: "user"}, {ID: "reply", Kind: "assistant"}}
	v = s.Snapshot()
	if len(v.Items) != 3 || v.Items[0].ID != "native-first" || v.Items[1].RequestID != "second" {
		t.Fatalf("native reconciliation moved pending input: %+v", v.Items)
	}
}

func TestTerminalOutgoingDoesNotMoveToTailAfterHistoryReplacement(t *testing.T) {
	e := &delayedInput{view: api.Snapshot{Items: []api.Item{{ID: "anchor", Kind: "assistant", Text: "old"}}}}
	s := NewService(e, nil, nil, nil, nil)
	s.stageOutgoing(api.Submission{ID: "rejected-original", Text: "failed input"}, nil)
	s.finishOutgoing("rejected-original", api.Receipt{ID: "rejected-original", Outcome: "rejected"})
	if got := s.Snapshot().Items; len(got) != 2 || got[1].Status != "rejected" {
		t.Fatalf("original terminal display missing: %+v", got)
	}
	e.view.Items = []api.Item{{ID: "newer", Kind: "assistant", Text: "recent history"}}
	if got := s.Snapshot().Items; len(got) != 1 || got[0].ID != "newer" || len(s.outbox) != 0 {
		t.Fatalf("rejected input reappeared after bounded history replacement: %+v", got)
	}
	s.stageOutgoing(api.Submission{ID: "unknown-original", Text: "uncertain input"}, nil)
	s.finishOutgoing("unknown-original", api.Receipt{ID: "unknown-original", Outcome: "unknown"})
	e.view.Items = []api.Item{{ID: "latest", Kind: "assistant", Text: "later history"}}
	if got := s.Snapshot().Items; len(got) != 2 || got[1].RequestID != "unknown-original" || got[1].Status != "unknown" {
		t.Fatalf("unknown original receipt was erased or replayed: %+v", got)
	}
}
