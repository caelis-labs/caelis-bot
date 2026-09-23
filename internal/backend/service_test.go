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
