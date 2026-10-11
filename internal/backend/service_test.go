package backend

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"path/filepath"
	"testing"
)

type imReceiptEngine struct {
	api.Engine
	value     api.Snapshot
	submitted api.Submission
}

func (e *imReceiptEngine) Snapshot() api.Snapshot { return e.value }
func (e *imReceiptEngine) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	e.submitted = in
	return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
}

func TestDesktopQuotedDraftReachesModelWithoutChangingVisibleBody(t *testing.T) {
	e := &imReceiptEngine{}
	draftPath := filepath.Join(t.TempDir(), "draft.json")
	s := NewService(e, func([]string) ([]api.InputFile, error) { return nil, nil }, nil, nil, nil)
	if err := s.ConfigureDraft(draftPath); err != nil {
		t.Fatal(err)
	}
	quote := &api.QuotedMessage{LocalID: "assistant-1", Role: "assistant", Text: "原文 <reference> 😀"}
	d, err := s.SaveDraft(api.Draft{Text: "本次正文", Quoted: quote})
	if err != nil || d.Quoted == nil || d.Quoted.Text != quote.Text {
		t.Fatal("quote not saved in draft", err)
	}
	s = NewService(e, func([]string) ([]api.InputFile, error) { return nil, nil }, nil, nil, nil)
	if err := s.ConfigureDraft(draftPath); err != nil {
		t.Fatal(err)
	}
	d = s.Draft()
	if d.Quoted == nil || d.Quoted.Text != quote.Text {
		t.Fatalf("quoted draft not restored: %+v", d)
	}
	s.ConfigureChat(filepath.Join(t.TempDir(), "chat.sqlite"))
	defer s.chat.Close()
	input := api.Submission{ID: "desktop-quoted", Text: d.Text, Quoted: d.Quoted}
	receipt, err := s.Submit(t.Context(), input)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	if got := e.submitted.ModelInputText(); got != "<reference>\n原文 &lt;reference&gt; 😀\n</reference>\n\n本次正文" {
		t.Fatalf("native model input = %q", got)
	}
	items := s.ChatSnapshot(0, "").Snapshot.Items
	if len(items) != 1 || items[0].Text != "本次正文" || items[0].RequestID != input.ID {
		t.Fatalf("visible transcript = %+v", items)
	}
	if next := s.Draft(); next.Text != "" || next.Quoted != nil {
		t.Fatalf("accepted draft not cleared: %+v", next)
	}
}

func TestLocalIMReadDoesNotImportNativeHistoryAndRecordsAcceptedInput(t *testing.T) {
	e := &imReceiptEngine{value: api.Snapshot{Items: []api.Item{{ID: "old", Kind: "assistant", Text: "native history"}}}}
	s := NewService(e, func([]string) ([]api.InputFile, error) { return nil, nil }, nil, nil, nil)
	s.ConfigureChat(filepath.Join(t.TempDir(), "chat.sqlite"))
	defer s.chat.Close()
	if got := s.ChatSnapshot(0, "").Snapshot.Items; len(got) != 0 {
		t.Fatalf("read imported Runtime history: %+v", got)
	}
	if receipt, err := s.Submit(t.Context(), api.Submission{ID: "original", Text: "user text"}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	got := s.ChatSnapshot(0, "").Snapshot.Items
	if len(got) != 1 || got[0].RequestID != "original" || got[0].Text != "user text" || got[0].Status != "accepted" {
		t.Fatalf("accepted input waited for Runtime echo: %+v", got)
	}
}

type snapshotEngine struct {
	api.Engine
	value api.Snapshot
}

func (e snapshotEngine) Snapshot() api.Snapshot { return e.value }

func TestSetupRequiredProjectsExplicitConnectionIssue(t *testing.T) {
	s := NewService(snapshotEngine{value: api.Snapshot{Connection: "offline"}}, nil, nil, nil, nil)
	s.RequireSetup(true)
	if got := s.Snapshot(); got.ConnectionIssue != "setup_required" || got.Connection != "offline" {
		t.Fatalf("missing runtime choice projected as %+v", got)
	}
	s.RequireSetup(false)
	if got := s.Snapshot(); got.ConnectionIssue != "" {
		t.Fatalf("stale setup issue after configuration: %+v", got)
	}
}
func TestControlReceiptAppearsInDesktopChatWithoutReplacingNativeItems(t *testing.T) {
	s := NewService(snapshotEngine{value: api.Snapshot{Items: []api.Item{{ID: "reply", Kind: "assistant", Text: "work result", SeenAt: 1}}}}, nil, nil, nil, nil)
	var notices []api.Item
	s.SetControlNotices(func() ([]api.Item, uint64) { return notices, uint64(len(notices)) })
	first := s.ChatSnapshot(0, "")
	if !first.Changed || len(first.Snapshot.Items) != 1 {
		t.Fatal(first)
	}
	notices = []api.Item{{ID: "control:1", Kind: "controlNotice", Text: "决定已提交", Status: "completed", SeenAt: 2}}
	next := s.ChatSnapshot(first.Snapshot.Revision, first.Snapshot.BotStatus)
	if !next.Changed || len(next.Snapshot.Items) != 2 || next.Snapshot.Items[0].Text != "work result" || next.Snapshot.Items[1].Text != "决定已提交" {
		t.Fatal(next)
	}
	for _, item := range s.PetSnapshot().Items {
		if item.Kind == "controlNotice" {
			t.Fatal("control receipt opened a pet bubble")
		}
	}
}

func TestDesktopChatInterleavesReceiptsAndMovesUntimedLegacyReceiptsBeforeNewReply(t *testing.T) {
	s := NewService(snapshotEngine{value: api.Snapshot{Items: []api.Item{
		{ID: "first", Kind: "user", Text: "request", SeenAt: 100},
		{ID: "last", Kind: "assistant", Text: "result", SeenAt: 300},
	}}}, nil, nil, nil, nil)
	s.SetControlNotices(func() ([]api.Item, uint64) {
		return []api.Item{
			{ID: "legacy", Kind: "controlNotice", Text: "old receipt"},
			{ID: "middle", Kind: "controlNotice", Text: "decision accepted", SeenAt: 200},
		}, 2
	})
	got := s.ChatSnapshot(0, "").Snapshot.Items
	if len(got) != 4 || got[0].ID != "legacy" || got[1].ID != "first" || got[2].ID != "middle" || got[3].ID != "last" {
		t.Fatalf("desktop transcript order: %+v", got)
	}
	if raw := s.Snapshot().Items; len(raw) != 4 || raw[0].ID != "first" || raw[1].ID != "last" {
		t.Fatalf("presentation sort changed Runtime item order: %+v", raw)
	}
}

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
