package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestAcceptedAttachmentDraftWriteFailureKeepsOriginalJournalAndRecovers(t *testing.T) {
	root := t.TempDir()
	path, blocked := filepath.Join(root, "draft.json"), filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	e := &delayedInput{}
	selected := []string{"sent-image"}
	s := NewService(e, func(ids []string) ([]api.InputFile, error) {
		if len(ids) != 1 || !slices.Contains(selected, ids[0]) {
			return nil, errors.New("expired")
		}
		return []api.InputFile{{Name: "image.png"}}, nil
	}, nil, nil, nil)
	if err := s.ConfigureDraft(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(api.Draft{Text: "caption"}); err != nil {
		t.Fatal(err)
	}
	s.consumeFiles = func(ids []string) error {
		selected = slices.DeleteFunc(selected, func(id string) bool { return slices.Contains(ids, id) })
		return nil
	}
	s.SetUserSubmitter(func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		s.draftFile = blocked // The pre-dispatch journal was durably written at path.
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	})
	input := api.Submission{ID: "original", Text: "caption", FileIDs: []string{"sent-image"}}
	if receipt, err := s.Submit(t.Context(), input); err != nil || receipt.Outcome != "accepted" {
		t.Fatalf("receipt: %+v %v", receipt, err)
	}
	got := s.Draft()
	if !got.CleanupPending || got.Text != "" || got.Notice == "" || !slices.Equal(got.ConsumedFileIDs, input.FileIDs) || len(selected) != 0 {
		t.Fatalf("accepted failure was not projected safely: %+v selected=%v", got, selected)
	}
	if receipt, _ := s.Submit(t.Context(), api.Submission{ID: "replacement", Text: "caption", FileIDs: input.FileIDs}); receipt.Outcome != "rejected" {
		t.Fatalf("consumed file was submitted again: %+v", receipt)
	}
	// A restart with no original native receipt must preserve uncertainty and
	// reject replacement sends. It does not infer acceptance from an old draft.
	restored := NewService(e, func(ids []string) ([]api.InputFile, error) {
		if len(ids) != 1 || !slices.Contains(selected, ids[0]) {
			return nil, errors.New("expired")
		}
		return []api.InputFile{{Name: "image.png"}}, nil
	}, nil, nil, nil)
	if err := restored.ConfigureDraft(path); err != nil {
		t.Fatal(err)
	}
	if d := restored.Draft(); !d.PendingSend || d.Text != "caption" {
		t.Fatalf("lost unconfirmed original after restart: %+v", d)
	}
	if receipt, _ := restored.Submit(t.Context(), api.Submission{ID: "new-id", FileIDs: input.FileIDs}); receipt.Outcome != "rejected" {
		t.Fatalf("restart replayed original file: %+v", receipt)
	}
	restored.consumeFiles = func([]string) error { return nil }
	e.mu.Lock()
	e.view.LastReceipt = api.Receipt{ID: "original", Outcome: "accepted"}
	e.mu.Unlock()
	if d := restored.Draft(); d.CleanupPending || d.PendingSend || d.Text != "" {
		t.Fatalf("original receipt did not reconcile: %+v", d)
	}
}

func TestAcceptedSelectionFailureKeepsNewDraftAndOnlyConsumesOriginalBatch(t *testing.T) {
	root := t.TempDir()
	e := &delayedInput{}
	selected := []string{"old-image"}
	consumptionFails := true
	s := NewService(e, func(ids []string) ([]api.InputFile, error) {
		if len(ids) != 1 || !slices.Contains(selected, ids[0]) {
			return nil, errors.New("expired")
		}
		return []api.InputFile{{Name: ids[0]}}, nil
	}, nil, nil, nil)
	if err := s.ConfigureDraft(filepath.Join(root, "draft.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(api.Draft{Text: "old caption"}); err != nil {
		t.Fatal(err)
	}
	s.consumeFiles = func(ids []string) error {
		if consumptionFails {
			return errors.New("selection write failed")
		}
		selected = slices.DeleteFunc(selected, func(id string) bool { return slices.Contains(ids, id) })
		return nil
	}
	s.SetUserSubmitter(func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		selected = append(selected, "new-image")
		if _, err := s.SaveDraft(api.Draft{Revision: 1, Text: "new draft"}); err != nil {
			t.Fatal(err)
		}
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	})
	if r, err := s.Submit(t.Context(), api.Submission{ID: "original", Text: "old caption", FileIDs: []string{"old-image"}}); err != nil || r.Outcome != "accepted" {
		t.Fatalf("receipt: %+v %v", r, err)
	}
	if d := s.Draft(); !d.CleanupPending || d.Text != "new draft" || !slices.Equal(d.ConsumedFileIDs, []string{"old-image"}) || d.Notice == "" {
		t.Fatalf("selection failure lost new draft or receipt: %+v", d)
	}
	if !slices.Equal(selected, []string{"old-image", "new-image"}) {
		t.Fatalf("selection changed despite failed write: %v", selected)
	}
	if r, _ := s.Submit(t.Context(), api.Submission{ID: "replacement", FileIDs: []string{"old-image"}}); r.Outcome != "rejected" {
		t.Fatalf("old file dispatched: %+v", r)
	}
	// Accepted marker was persisted before selection cleanup; restart can retry
	// the same original batch after the storage fault is repaired.
	restored := NewService(e, func(ids []string) ([]api.InputFile, error) {
		if len(ids) != 1 || !slices.Contains(selected, ids[0]) {
			return nil, errors.New("expired")
		}
		return []api.InputFile{{Name: ids[0]}}, nil
	}, nil, nil, nil)
	if err := restored.ConfigureDraft(filepath.Join(root, "draft.json")); err != nil {
		t.Fatal(err)
	}
	consumptionFails = false
	restored.consumeFiles = func(ids []string) error {
		selected = slices.DeleteFunc(selected, func(id string) bool { return slices.Contains(ids, id) })
		return nil
	}
	if d := restored.Draft(); d.CleanupPending || d.PendingSend || d.Text != "new draft" {
		t.Fatalf("cleanup retry lost concurrent draft: %+v", d)
	}
	if !slices.Equal(selected, []string{"new-image"}) {
		t.Fatalf("wrong batch consumed: %v", selected)
	}
	if r, _ := restored.Submit(t.Context(), api.Submission{ID: "cannot-resend", FileIDs: []string{"old-image"}}); r.Outcome != "rejected" {
		t.Fatalf("consumed file reused after recovery: %+v", r)
	}
}

func TestAttachmentJournalFailureRefusesDispatchAndUnknownKeepsOriginalID(t *testing.T) {
	root := t.TempDir()
	path, blocked := filepath.Join(root, "draft.json"), filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	e := &delayedInput{}
	selected := []string{"image"}
	s := NewService(e, func([]string) ([]api.InputFile, error) { return []api.InputFile{{Name: "image.png"}}, nil }, nil, nil, nil)
	if err := s.ConfigureDraft(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(api.Draft{Text: "caption"}); err != nil {
		t.Fatal(err)
	}
	dispatched := 0
	s.SetUserSubmitter(func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		dispatched++
		return api.Receipt{ID: in.ID, Outcome: "unknown"}, nil
	})
	s.draftFile = blocked
	input := api.Submission{ID: "original", Text: "caption", FileIDs: selected}
	if r, _ := s.Submit(t.Context(), input); r.Outcome != "rejected" || dispatched != 0 {
		t.Fatalf("uncommitted journal dispatched: %+v calls=%d", r, dispatched)
	}
	if d := s.Draft(); d.Text != "caption" || d.Revision != 1 || d.PendingSend {
		t.Fatalf("pre-dispatch failure changed draft: %+v", d)
	}
	s.draftFile = path
	if r, _ := s.Submit(t.Context(), input); r.Outcome != "unknown" || dispatched != 1 {
		t.Fatalf("unknown receipt: %+v calls=%d", r, dispatched)
	}
	if d := s.Draft(); !d.PendingSend || d.Text != "caption" {
		t.Fatalf("unknown original was discarded: %+v", d)
	}
	restored := NewService(e, func(ids []string) ([]api.InputFile, error) {
		if len(ids) != 1 || !slices.Contains(selected, ids[0]) {
			return nil, errors.New("expired")
		}
		return []api.InputFile{{Name: "image.png"}}, nil
	}, nil, nil, nil)
	if err := restored.ConfigureDraft(path); err != nil {
		t.Fatal(err)
	}
	if d := restored.Draft(); !d.PendingSend {
		t.Fatalf("unknown not restored: %+v", d)
	}
	if r, _ := restored.Submit(t.Context(), api.Submission{ID: "replacement", FileIDs: selected}); r.Outcome != "rejected" {
		t.Fatalf("unknown replayed under new ID: %+v", r)
	}
	restored.consumeFiles = func(ids []string) error {
		selected = slices.DeleteFunc(selected, func(id string) bool { return slices.Contains(ids, id) })
		return nil
	}
	e.mu.Lock()
	e.view.Items = []api.Item{{ID: "native-original", Kind: "user", RequestID: "original"}}
	e.mu.Unlock()
	if d := restored.Draft(); d.PendingSend || d.CleanupPending || len(selected) != 0 {
		t.Fatalf("native original input did not reconcile unknown: %+v selected=%v", d, selected)
	}
}

func TestRejectedAttachmentRetainsDraftAndSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draft.json")
	e := draftEngine{outcome: "rejected"}
	selected := []string{"image"}
	s := NewService(e, func([]string) ([]api.InputFile, error) { return []api.InputFile{{Name: "image.png"}}, nil }, nil, nil, nil)
	if err := s.ConfigureDraft(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(api.Draft{Text: "caption"}); err != nil {
		t.Fatal(err)
	}
	s.consumeFiles = func([]string) error { t.Fatal("rejected input consumed selection"); return nil }
	if r, err := s.Submit(t.Context(), api.Submission{ID: "rejected-original", Text: "caption", FileIDs: selected}); err != nil || r.Outcome != "rejected" {
		t.Fatalf("receipt: %+v %v", r, err)
	}
	if d := s.Draft(); d.Text != "caption" || d.CleanupPending || d.PendingSend || !slices.Equal(selected, []string{"image"}) {
		t.Fatalf("rejected input changed draft: %+v selected=%v", d, selected)
	}
	restored := NewService(e, nil, nil, nil, nil)
	if err := restored.ConfigureDraft(path); err != nil {
		t.Fatal(err)
	}
	if d := restored.Draft(); d.Text != "caption" || d.PendingSend {
		t.Fatalf("rejected draft not restored: %+v", d)
	}
}

func TestPreDispatchRecoveryRefusalReleasesAttachmentClaim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draft.json")
	s := NewService(&delayedInput{}, func([]string) ([]api.InputFile, error) { return []api.InputFile{{Name: "image.png"}}, nil }, func([]string) error { t.Fatal("pre-dispatch refusal consumed file"); return nil }, nil, nil)
	if err := s.ConfigureDraft(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(api.Draft{Text: "caption"}); err != nil {
		t.Fatal(err)
	}
	s.SetUserSubmitter(func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
		return api.Receipt{}, api.ErrRecoveryPending
	})
	if _, err := s.Submit(t.Context(), api.Submission{ID: "original", Text: "caption", FileIDs: []string{"image"}}); !errors.Is(err, api.ErrRecoveryPending) {
		t.Fatalf("pre-dispatch outcome: %v", err)
	}
	if d := s.Draft(); d.Text != "caption" || d.PendingSend || d.CleanupPending || len(s.outbox) != 0 {
		t.Fatalf("pre-dispatch refusal retained false claim: %+v outbox=%+v", d, s.outbox)
	}
}

func TestAcceptedImageReceiptRecoversDraftJournalAfterRestartWithoutNativeSnapshot(t *testing.T) {
	root := t.TempDir()
	path, blocked := filepath.Join(root, "draft.json"), filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(root, "MessageMedia")
	file := testMessagePNG(t, "pasted.png")
	e := &delayedInput{}
	selected := true
	resolve := func([]string) ([]api.InputFile, error) {
		if !selected {
			return nil, errors.New("expired")
		}
		return []api.InputFile{file}, nil
	}
	s := NewService(e, resolve, nil, nil, nil)
	if err := ConfigureMessageMedia(s, media); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfigureDraft(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(api.Draft{Text: "caption"}); err != nil {
		t.Fatal(err)
	}
	s.consumeFiles = func([]string) error { selected = false; return nil }
	s.SetUserSubmitter(func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		s.draftFile = blocked
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	})
	if r, err := s.Submit(t.Context(), api.Submission{ID: "original", Text: "caption", FileIDs: []string{"image"}}); err != nil || r.Outcome != "accepted" {
		t.Fatalf("receipt: %+v %v", r, err)
	}
	if status := s.messageMedia.ReceiptStatus("original"); status != "accepted" {
		t.Fatalf("original media receipt not durable: %q", status)
	}
	// The draft file still has the pre-dispatch claim; media has the same
	// request's accepted receipt even if native recent history is absent.
	restored := NewService(e, resolve, nil, nil, nil)
	if err := ConfigureMessageMedia(restored, media); err != nil {
		t.Fatal(err)
	}
	if err := restored.ConfigureDraft(path); err != nil {
		t.Fatal(err)
	}
	restored.consumeFiles = func([]string) error { selected = false; return nil }
	if d := restored.Draft(); d.PendingSend || d.CleanupPending || d.Text != "" {
		t.Fatalf("durable accepted receipt did not recover draft: %+v", d)
	}
}

type acceptedDuringRead struct {
	delayedInput
	acknowledged chan struct{}
	finish       chan struct{}
}

func (e *acceptedDuringRead) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	r := api.Receipt{ID: in.ID, Outcome: "accepted"}
	e.mu.Lock()
	e.view.LastReceipt = r
	e.mu.Unlock()
	close(e.acknowledged)
	<-e.finish
	return r, nil
}

func TestAcceptedAttachmentBatchConsumedOnceAcrossSnapshotAndKeepsNewEdits(t *testing.T) {
	e := &acceptedDuringRead{acknowledged: make(chan struct{}), finish: make(chan struct{})}
	selected := []string{"pasted-image"}
	consumeCalls := 0
	s := NewService(e, func(ids []string) ([]api.InputFile, error) {
		if !slices.Equal(ids, []string{"pasted-image"}) {
			t.Fatalf("wrong staged batch: %v", ids)
		}
		return []api.InputFile{{Name: "pasted.png"}}, nil
	}, func(ids []string) error {
		consumeCalls++
		selected = slices.DeleteFunc(selected, func(id string) bool { return slices.Contains(ids, id) })
		return nil
	}, nil, nil)
	if err := s.ConfigureDraft(filepath.Join(t.TempDir(), "draft.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(api.Draft{Text: "caption"}); err != nil {
		t.Fatal(err)
	}
	done := make(chan api.Receipt, 1)
	go func() {
		r, _ := s.Submit(t.Context(), api.Submission{ID: "original-image-send", Text: "caption", FileIDs: []string{"pasted-image"}})
		done <- r
	}()
	<-e.acknowledged
	selected = append(selected, "new-drop")
	if _, err := s.SaveDraft(api.Draft{Revision: 1, Text: "new draft"}); err != nil {
		t.Fatal(err)
	}
	before := s.Draft().Revision
	s.Snapshot() // Native accepted receipt can arrive before Submit returns.
	close(e.finish)
	if r := <-done; r.Outcome != "accepted" {
		t.Fatal(r)
	}
	if consumeCalls != 1 || !slices.Equal(selected, []string{"new-drop"}) || s.Draft().Text != "new draft" || s.Draft().Revision != before || s.pendingDraft != nil {
		t.Fatalf("accepted batch consumed twice or newer input erased: calls=%d files=%v draft=%+v", consumeCalls, selected, s.Draft())
	}
}

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
			s := NewService(e, func([]string) ([]api.InputFile, error) { return []api.InputFile{{Name: "attached.txt"}}, nil }, func([]string) error { return nil }, nil, nil)
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
