package backend

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestChatCardClaimsAndReopensOriginalInput(t *testing.T) {
	const id = "a2ui-0123456789abcdef0123456789abcdef-abcdef0123456789abcdef0123456789-0"
	const message = "查一下上海浦东未来七天的天气"
	fields := []api.ChatCardField{{Name: "城市", Value: "上海浦东"}, {Name: "日期", Value: "未来七天"}}
	path := filepath.Join(t.TempDir(), "chat.sqlite")
	engine := &imReceiptEngine{}
	s := NewService(engine, nil, nil, nil, nil)
	s.ConfigureChat(path)
	draft, err := s.SaveDraft(api.Draft{Text: "unsent"})
	if err != nil {
		t.Fatal(err)
	}
	s.SetCommandHandler(func(context.Context, api.Submission) (api.Receipt, bool, error) {
		t.Error("card input reached command handling")
		return api.Receipt{}, false, nil
	})
	entered, finish := make(chan struct{}), make(chan struct{})
	s.SetUserSubmitter(func(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
		if in.ID != id || in.ModelInputText() != message || len(files) != 0 || in.HostNotice {
			t.Errorf("input did not take ordinary user path: %+v", in)
		}
		close(entered)
		<-finish
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	})
	sent := make(chan api.Receipt, 1)
	go func() {
		receipt, err := s.SubmitChatCard(t.Context(), id, message, fields)
		if err != nil {
			t.Error(err)
		}
		sent <- receipt
	}()
	<-entered
	duplicate, err := s.SubmitChatCard(t.Context(), id, message, fields)
	close(finish)
	if err != nil || duplicate.Outcome != "unknown" || duplicate.ID != id {
		t.Fatalf("concurrent duplicate = %+v, %v", duplicate, err)
	}
	if receipt := <-sent; receipt.Outcome != "accepted" {
		t.Fatalf("first receipt = %+v", receipt)
	}
	if got := s.Draft(); got.Text != draft.Text || got.Revision != draft.Revision {
		t.Fatalf("changed composer: %+v", got)
	}
	// Native echoes have no card metadata. The receipt and original fields
	// travel with the same normal chat item after process/history reload.
	s.ObserveChat(api.Snapshot{Items: []api.Item{
		{Kind: "user", ID: "echo", RequestID: id, Status: "completed", Text: message},
	}})
	s.chat.Close()

	reopened := NewService(engine, nil, nil, nil, nil)
	reopened.ConfigureChat(path)
	defer reopened.chat.Close()
	reopened.SetUserSubmitter(func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
		t.Fatal("reopened card dispatched the same input again")
		return api.Receipt{}, nil
	})
	receipt, err := reopened.SubmitChatCard(t.Context(), id, message, fields)
	if err != nil || receipt.Outcome != "accepted" || receipt.ID != id {
		t.Fatalf("reopen receipt = %+v, %v", receipt, err)
	}
	items := reopened.ChatSnapshot(0, "", "").Snapshot.Items
	if len(items) != 1 || items[0].Text != message || items[0].ChatCard == nil || len(items[0].ChatCard.Fields) != 2 || items[0].ChatCard.Fields[0] != fields[0] {
		t.Fatalf("history lost original snapshot: %+v", items)
	}
}

func TestChatCardNeverReplaysUnknownDispatch(t *testing.T) {
	const id = "a2ui-0123456789abcdef0123456789abcdef-abcdef0123456789abcdef0123456789-0"
	path := filepath.Join(t.TempDir(), "chat.sqlite")
	calls := 0
	for attempt := 0; attempt < 2; attempt++ {
		s := NewService(&imReceiptEngine{}, nil, nil, nil, nil)
		s.ConfigureChat(path)
		s.SetUserSubmitter(func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
			calls++
			return api.Receipt{}, errors.New("transport ended")
		})
		receipt, err := s.SubmitChatCard(t.Context(), id, "Check the current value", nil)
		s.chat.Close()
		if err != nil || receipt.ID != id || receipt.Outcome != "unknown" {
			t.Fatalf("attempt %d: %+v, %v", attempt, receipt, err)
		}
	}
	if calls != 1 {
		t.Fatalf("dispatched %d times", calls)
	}
}
