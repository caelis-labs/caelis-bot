package backend

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestChatPublishesInputBeforeRetiringOutgoing(t *testing.T) {
	for _, introduction := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "introduction"}[introduction], func(t *testing.T) {
			for _, receiptOutcome := range []string{"", "accepted", "rejected"} {
				name := receiptOutcome
				if name == "" {
					name = "without_receipt"
				}
				t.Run(name, func(t *testing.T) {
					e := &imReceiptEngine{}
					s := NewService(e, nil, nil, nil, nil)
					s.ConfigureChat(filepath.Join(t.TempDir(), "chat.sqlite"))
					defer s.chat.Close()
					in := api.Submission{ID: "original-request", Text: "retained input"}
					s.stageOutgoing(in, nil)
					if introduction {
						s.initializer = presentationInitializer{api.BotInitialization{Status: "dispatching", RequestID: in.ID, Prompt: in.Text}}
					}
					if got := s.Snapshot(); len(got.Items) != 1 {
						t.Fatal("input must appear before native acceptance")
					}
					// The adapter's echo and acceptance can precede the application's
					// independent ObserveChat loop and the Submit response.
					e.value = api.Snapshot{Connection: "ready", Phase: "working", LastReceipt: api.Receipt{ID: in.ID, Outcome: receiptOutcome}, Items: []api.Item{{ID: "native-input", RequestID: in.ID, Kind: "user", Text: in.Text}}}
					if introduction {
						s.initializer = presentationInitializer{api.BotInitialization{Status: "accepted", RequestID: in.ID}}
					}
					v := s.Snapshot()
					if len(v.Items) != 1 || v.Items[0].RequestID != in.ID || v.Items[0].Text != in.Text {
						t.Fatalf("native input disappeared before IM observation: %+v", v.Items)
					}
					outcome := receiptOutcome
					if outcome == "" {
						outcome = "accepted"
					}
					s.recordInput(in, nil, api.Receipt{ID: in.ID, Outcome: outcome})
					s.ObserveChat(e.value)
					if got := s.Snapshot(); len(got.Items) != 1 || got.Items[0].RequestID != in.ID {
						t.Fatal("late receipt/observer duplicated input")
					}
					// Once history publishes the original input, the temporary row must
					// retire rather than accumulating beside the persisted message.
					deadline := time.Now().Add(5 * time.Second)
					for {
						got := s.Snapshot()
						s.mu.Lock()
						remaining := len(s.outbox)
						s.mu.Unlock()
						if len(got.Items) != 1 || got.Items[0].RequestID != in.ID || got.Items[0].Text != in.Text {
							t.Fatalf("publication changed the original input: %+v", got.Items)
						}
						if remaining == 0 {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("published input did not retire outgoing row")
						}
						time.Sleep(time.Millisecond)
					}
				})
			}
		})
	}
}
