package backend

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"path/filepath"
	"testing"
)

type snapshotEngine struct {
	api.Engine
	value api.Snapshot
}

func (e snapshotEngine) Snapshot() api.Snapshot { return e.value }

func TestPetPreviewDoesNotMixOldReplyWithNewRequest(t *testing.T) {
	e := snapshotEngine{value: api.Snapshot{Items: []api.Item{
		{ID: "old-user", Kind: "user", Text: "old"}, {ID: "old-reply", Kind: "assistant", Text: "old reply"},
		{ID: "new-user", Kind: "user", Text: "new"}, {ID: "tool1", Kind: "activity", Text: "first"}, {ID: "tool2", Kind: "activity", Text: "latest", Details: "full output"},
	}, Approvals: []api.Approval{{ID: "resolved", Status: "resolved"}, {ID: "pending", Title: "confirm", Status: "pending", Details: "command", Choices: []api.Choice{{ID: "once"}}}}}}
	s := NewService(e, nil, nil, nil, nil)
	got := s.PetSnapshot()
	if len(got.Items) != 2 || got.Items[0].ID != "new-user" || got.Items[1].ID != "tool2" || got.Items[1].Details != "" {
		t.Fatalf("unexpected preview: %+v", got.Items)
	}
	if len(got.Approvals) != 1 || got.Approvals[0].ID != "pending" || len(got.Approvals[0].Choices) != 1 {
		t.Fatal("expanded bubble must preserve the exact decision choices")
	}
	if full := s.Snapshot(); len(full.Items) != 5 || full.Items[4].Details != "full output" || len(full.Approvals[1].Choices) != 1 {
		t.Fatal("preview mutated authoritative history")
	}
}

func TestDraftFenceAndAcceptedClearPreserveNewEdits(t *testing.T) {
	s := NewService(snapshotEngine{}, nil, nil, nil, nil)
	d, e := s.SaveDraft(api.Draft{Text: "old", ReferenceIDs: []string{}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDraft(api.Draft{Text: "stale"}); e == nil {
		t.Fatal("stale renderer overwrote draft")
	}
	d.Text = "new"
	if _, e = s.SaveDraft(d); e != nil {
		t.Fatal(e)
	}
	s.clearDraft(api.Submission{Text: "old", ReferenceIDs: []string{}})
	if s.Draft().Text != "new" {
		t.Fatal("late receipt erased new input")
	}
}
func TestPreviewAcknowledgementPersistsAndCannotDismissWorkOrNewResult(t *testing.T) {
	e := &snapshotEngine{value: api.Snapshot{Phase: "completed", Items: []api.Item{{ID: "user", Kind: "user"}, {ID: "answer", Kind: "assistant", Text: "result"}}}}
	s := NewService(e, nil, nil, nil, nil)
	path := filepath.Join(t.TempDir(), "ack.json")
	if err := s.ConfigurePresentation(path); err != nil {
		t.Fatal(err)
	}
	key := s.Snapshot().PreviewKey
	if err := s.DismissPreview(key); err != nil {
		t.Fatal(err)
	}
	restored := NewService(e, nil, nil, nil, nil)
	_ = restored.ConfigurePresentation(path)
	if !restored.Snapshot().PreviewDismissed {
		t.Fatal("dismissed outcome reappeared")
	}
	e.value.Items[1].Text = "new result"
	if restored.Snapshot().PreviewDismissed || restored.DismissPreview(key) == nil {
		t.Fatal("old acknowledgement hid a newer result")
	}
	e.value.CanInterrupt = true
	if restored.DismissPreview(restored.Snapshot().PreviewKey) == nil {
		t.Fatal("dismissed active work")
	}
}

func TestPetPreviewDismissUsesDisplayedResultWithOutgoingReceipts(t *testing.T) {
	e := &snapshotEngine{value: api.Snapshot{Phase: "completed", Items: []api.Item{
		{ID: "user-1", Kind: "user", RequestID: "request-1"},
		{ID: "reply-1", Kind: "assistant", Text: "answer"},
	}}}
	s := NewService(e, nil, nil, nil, nil)
	if err := ConfigureMessageMedia(s, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ack.json")
	if err := s.ConfigurePresentation(path); err != nil {
		t.Fatal(err)
	}
	first := s.PetSnapshot().PreviewKey
	e.value.Revision++
	if first != s.PetSnapshot().PreviewKey {
		t.Fatal("unrelated revision changed the displayed result")
	}
	// An uncertain original request is kept under its original ID. A native
	// reply appearing after it is the last visible result, across both views.
	e.value.Items = e.value.Items[:1]
	s.stageOutgoing(api.Submission{ID: "uncertain", Text: "pending"}, nil)
	s.finishOutgoing("uncertain", api.Receipt{ID: "uncertain", Outcome: "unknown"})
	e.value.Items = append(e.value.Items, api.Item{ID: "reply-1", Kind: "assistant", Text: "answer"})
	key := s.PetSnapshot().PreviewKey
	if err := s.DismissPreview(key); err != nil {
		t.Fatal("displayed answer was rejected:", err)
	}
	restored := NewService(e, nil, nil, nil, nil)
	if err := ConfigureMessageMedia(restored, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := restored.ConfigurePresentation(path); err != nil || !restored.PetSnapshot().PreviewDismissed {
		t.Fatal("acknowledgement did not persist", err)
	}
	e.value.Items = append(e.value.Items, api.Item{ID: "user-2", Kind: "user", RequestID: "request-2", Text: "next"})
	if restored.PetSnapshot().PreviewDismissed || restored.DismissPreview(key) == nil {
		t.Fatal("old acknowledgement hid a new request")
	}
	e.value.Approvals = []api.Approval{{ID: "approve", Status: "pending"}}
	if restored.DismissPreview(restored.PetSnapshot().PreviewKey) == nil {
		t.Fatal("pending approval was dismissed")
	}
}

func TestPetPreviewPreservesAssistantIdentityAcrossOriginalReceiptOutcomes(t *testing.T) {
	for _, outcome := range []string{"unknown", "rejected", "accepted"} {
		t.Run(outcome, func(t *testing.T) {
			e := &snapshotEngine{value: api.Snapshot{Phase: "completed", Items: []api.Item{{ID: "first", Kind: "user", RequestID: "first-request"}}}}
			s := NewService(e, nil, nil, nil, nil)
			if err := ConfigureMessageMedia(s, t.TempDir()); err != nil {
				t.Fatal(err)
			}
			s.stageOutgoing(api.Submission{ID: "original-request", Text: "same prompt"}, nil)
			s.finishOutgoing("original-request", api.Receipt{ID: "original-request", Outcome: outcome})
			e.value.Items = append(e.value.Items, api.Item{ID: "answer", Kind: "assistant", Text: "same result"})
			key := s.PetSnapshot().PreviewKey
			if err := s.DismissPreview(key); err != nil {
				t.Fatal(err)
			}
			if outcome == "rejected" {
				e.value.Items = append(e.value.Items, api.Item{ID: "later", Kind: "assistant", Text: "new result"})
				if s.PetSnapshot().PreviewDismissed {
					t.Fatal("rejected input's later independent result inherited the old acknowledgement")
				}
				return
			}
			// Materialization removes the local bubble by exact request ID.
			e.value.Items = []api.Item{{ID: "first", Kind: "user", RequestID: "first-request"}, {ID: "native", Kind: "user", RequestID: "original-request"}, {ID: "answer", Kind: "assistant", Text: "same result"}}
			if got := s.PetSnapshot(); got.PreviewKey != key || !got.PreviewDismissed || len(s.outbox) != 0 {
				t.Fatalf("original receipt was replayed or changed the same answer: %+v", got.Items)
			}
		})
	}
}

func TestMissingOptionalCapabilitiesReturnUnavailable(t *testing.T) {
	s := NewService(snapshotEngine{}, nil, nil, nil, nil)
	ctx := context.Background()
	if _, err := s.Models(ctx); err == nil {
		t.Fatal("missing models reported success")
	}
	if _, err := s.ExecutionOptions(); err == nil {
		t.Fatal("missing policy reported success")
	}
	for _, err := range []error{s.Login(ctx), s.CancelLogin(ctx), s.RevealArtifact("unowned"), s.OpenApprovalURL("unowned"), s.OpenConnectionHelp()} {
		if err == nil {
			t.Fatal("missing optional capability reported success")
		}
	}
}

type interruptOrderEngine struct {
	api.Engine
	stopped *bool
	t       *testing.T
}

func (e interruptOrderEngine) Interrupt(context.Context) error {
	if !*e.stopped {
		e.t.Fatal("native input not stopped before runtime interrupt")
	}
	return nil
}
func TestUserStopCancelsNativeInputBeforeInterruptingRuntime(t *testing.T) {
	stopped := false
	s := NewService(interruptOrderEngine{stopped: &stopped, t: t}, nil, nil, nil, nil)
	s.SetInterruptObserver(func() { stopped = true })
	if err := s.Interrupt(t.Context()); err != nil {
		t.Fatal(err)
	}
}
