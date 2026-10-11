package textchannel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestAsyncQuestionCardAnswerMapsExactOptionAndSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	item := api.Item{ID: "native-agent-item", TurnKey: "completed-turn", Kind: "assistant", AsyncCallID: "call_native_17", AsyncQuestions: []api.AsyncQuestion{{Title: "选哪项？", Options: []string{"甲", "乙", "丙"}}}}
	card, err := s.AsyncCard(item, "runtime-owner-1")
	if err != nil || !strings.Contains(card.Text, "[Q1] 选哪项？\n[1] 甲\n[2] 乙\n[3] 丙\n\n回答请输入\n/answer Q1 1") {
		t.Fatalf("card %q, %v", card.Text, err)
	}
	if card.AsyncCallID != "" || len(card.AsyncQuestions) != 0 {
		t.Fatal("native handles escaped the display card")
	}
	var sent Inbound
	var prefix string
	s.SetAsyncAnswerer(func(_ context.Context, in Inbound, modelPrefix string) (api.Receipt, error) {
		sent, prefix = in, modelPrefix
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	})
	snapshot := api.Snapshot{RuntimeOwner: "runtime-owner-1", Connection: "ready"}
	command := strings.TrimSpace(card.Text[strings.LastIndex(card.Text, "\n/")+1:])
	command = strings.Replace(command, " 1", " 2", 1)
	in := Inbound{Channel: "telegram", Conversation: "paired", ID: "tg-1", Text: command}
	if feedback := s.Handle(t.Context(), in, snapshot); !strings.Contains(feedback, "已提交") {
		t.Fatal(feedback)
	}
	if sent != in || !strings.Contains(prefix, `"answer":"乙"`) || !strings.Contains(prefix, `"questionItemId":"[\"request_user_input_async\",\"call_native_17\",0]"`) {
		t.Fatalf("wrong exact answer binding: %#v %q", sent, prefix)
	}
	var payload []struct {
		Answer, Question, QuestionItemID string
	}
	encoded := strings.TrimSuffix(strings.TrimPrefix(prefix, "<send_user_message_question_reply>\n"), "\n</send_user_message_question_reply>")
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil || len(payload) != 1 || payload[0].Answer != "乙" || payload[0].Question != "选哪项？" || payload[0].QuestionItemID != `["request_user_input_async","call_native_17",0]` {
		t.Fatalf("invalid model reply envelope: %#v %v", payload, err)
	}
	reopened, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	reopened.SetAsyncAnswerer(func(context.Context, Inbound, string) (api.Receipt, error) {
		t.Fatal("duplicate dispatched")
		return api.Receipt{}, nil
	})
	if feedback := reopened.Handle(t.Context(), Inbound{Channel: "weixin", Conversation: "paired", ID: "wx-2", Text: command}, snapshot); !strings.Contains(feedback, "已提交") {
		t.Fatal(feedback)
	}
	card, err = reopened.AsyncCard(item, "runtime-owner-1")
	if err != nil || !strings.Contains(card.Text, "回答已提交") {
		t.Fatalf("card did not converge: %q %v", card.Text, err)
	}
}

func TestWorkerAsyncCardAnswersOriginalWorkAndIgnoresBotOwner(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	item := api.Item{ID: "worker-question:item", TurnKey: "worker-turn", Kind: "assistant", AsyncCallID: "native-worker-call", AsyncQuestions: []api.AsyncQuestion{{Title: "选项", Options: []string{"甲", "乙"}}}}
	card, err := s.AsyncWorkCard(item, "codex", "task-original")
	if err != nil || !strings.Contains(card.Text, "[Q1] 选项") {
		t.Fatal(card, err)
	}
	var workID, owner, itemID, input string
	var calls int
	s.SetAsyncWorkAnswerer(func(_ context.Context, _ Inbound, w, o, i, body string) (api.Receipt, error) {
		calls++
		workID, owner, itemID, input = w, o, i, body
		return api.Receipt{Outcome: "accepted"}, nil
	})
	command := strings.TrimSpace(card.Text[strings.LastIndex(card.Text, "\n/")+1:])
	command = strings.Replace(command, " 1", " 2", 1)
	for _, in := range []Inbound{{Channel: "telegram", Conversation: "paired", ID: "tg-worker-1", Text: command}, {Channel: "weixin", Conversation: "paired", ID: "wx-worker-2", Text: command}} {
		if feedback := s.Handle(t.Context(), in, api.Snapshot{RuntimeOwner: "different-bot-owner", Connection: "offline"}); !strings.Contains(feedback, "已提交") {
			t.Fatal(feedback)
		}
	}
	if calls != 1 || workID != "task-original" || owner != "codex" || itemID != item.ID || !strings.Contains(input, `"answer":"乙"`) || !strings.Contains(input, `"questionItemId":"[\"request_user_input_async\",\"native-worker-call\",0]"`) {
		t.Fatalf("worker answer lost original target or replayed: %d %q %q %q %q", calls, workID, owner, itemID, input)
	}
}

func TestGuardianAnswerKeepsResultOutOfUserNotices(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	item := api.Item{ID: "worker-question:private", TurnKey: "worker-turn", Kind: "assistant", AsyncCallID: "native-call", AsyncQuestions: []api.AsyncQuestion{{Title: "Choose", Options: []string{"甲", "乙"}}}}
	if _, err := s.AsyncWorkCard(item, "codex", "original-task"); err != nil {
		t.Fatal(err)
	}
	notices, calls := 0, 0
	s.SetNoticeObserver(func(Notice) { notices++ })
	s.SetAsyncWorkAnswerer(func(_ context.Context, _ Inbound, _, _, _, _ string) (api.Receipt, error) {
		calls++
		return api.Receipt{Outcome: "accepted"}, nil
	})
	first := s.HandleBotWorkAnswer(t.Context(), "worker-bot-answer", "Q1", "2")
	second := s.HandleBotWorkAnswer(t.Context(), "worker-bot-answer", "Q1", "2")
	if first.Outcome != "accepted" || second.Outcome != "accepted" || calls != 1 || notices != 0 || len(s.Notices()) != 0 {
		t.Fatal("private Guardian reply was duplicated or mirrored", first, second, calls, notices, s.Notices())
	}
}

func TestAsyncQuestionOwnerAndSchemaChangesInvalidateOldReference(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	item := api.Item{ID: "item", TurnKey: "turn", Kind: "assistant", AsyncCallID: "call", AsyncQuestions: []api.AsyncQuestion{{Title: "选择", Options: []string{"甲", "乙"}}}}
	_, _ = s.AsyncCard(item, "owner-1")
	s.SetAsyncAnswerer(func(context.Context, Inbound, string) (api.Receipt, error) {
		t.Fatal("stale answer dispatched")
		return api.Receipt{}, nil
	})
	in := Inbound{Channel: "telegram", Conversation: "paired", ID: "old-owner", Text: "/answer Q1 1"}
	if feedback := s.Handle(t.Context(), in, api.Snapshot{RuntimeOwner: "owner-2", Connection: "ready"}); !strings.Contains(feedback, "失效") {
		t.Fatal(feedback)
	}
	item.AsyncQuestions[0].Options = []string{"乙", "甲"}
	card, err := s.AsyncCard(item, "owner-1")
	if err != nil || !strings.Contains(card.Text, "[Q2]") {
		t.Fatalf("changed options reused index: %q %v", card.Text, err)
	}
	if feedback := s.Handle(t.Context(), Inbound{Channel: "telegram", Conversation: "paired", ID: "old-schema", Text: "/answer Q1 1"}, api.Snapshot{RuntimeOwner: "owner-1", Connection: "ready"}); !strings.Contains(feedback, "失效") {
		t.Fatal(feedback)
	}
}

func TestAsyncQuestionPredispatchRetryAndUnknownNeverReplay(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	item := api.Item{ID: "item", TurnKey: "turn", Kind: "assistant", AsyncCallID: "call", AsyncQuestions: []api.AsyncQuestion{{Title: "选择", Options: []string{"甲", "乙"}}}}
	_, _ = s.AsyncCard(item, "owner")
	snapshot := api.Snapshot{RuntimeOwner: "owner", Connection: "ready"}
	calls := 0
	s.SetAsyncAnswerer(func(_ context.Context, in Inbound, _ string) (api.Receipt, error) {
		calls++
		if calls == 1 {
			return api.Receipt{}, api.ErrRecoveryPending
		}
		return api.Receipt{ID: in.ID, Outcome: "unknown"}, nil
	})
	if feedback := s.Handle(t.Context(), Inbound{Channel: "telegram", Conversation: "paired", ID: "a", Text: "/answer Q1 1"}, snapshot); !strings.Contains(feedback, "未提交") {
		t.Fatal(feedback)
	}
	if feedback := s.Handle(t.Context(), Inbound{Channel: "telegram", Conversation: "paired", ID: "b", Text: "/answer Q1 2"}, snapshot); !strings.Contains(feedback, "待核对") || calls != 2 {
		t.Fatal(feedback, calls)
	}
	if feedback := s.Handle(t.Context(), Inbound{Channel: "weixin", Conversation: "paired", ID: "c", Text: "/answer Q1 1"}, snapshot); !strings.Contains(feedback, "待核对") || calls != 2 {
		t.Fatal(feedback, calls)
	}
}

func TestAsyncReplyTitleMatchesCodexByteBound(t *testing.T) {
	title := strings.Repeat("中", 171) + "\n后"
	bounded := asyncReplyTitle(title)
	if len(bounded) != 510 || strings.Contains(bounded, "\n") {
		t.Fatalf("title split a UTF-8 rune: %q", bounded)
	}
}

func TestAsyncMultipleQuestionsKeepIndependentFieldIDsAndFreeText(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	item := api.Item{ID: "item", TurnKey: "turn", Kind: "assistant", AsyncCallID: "call", AsyncQuestions: []api.AsyncQuestion{{Title: "第一题", Options: []string{"甲", "乙"}}, {Title: "第二题"}}}
	card, err := s.AsyncCard(item, "owner")
	if err != nil || !strings.Contains(card.Text, "[Q1] 第一题") || !strings.Contains(card.Text, "[Q2] 第二题") {
		t.Fatalf("multi-question card: %q %v", card.Text, err)
	}
	var modelInput string
	s.SetAsyncAnswerer(func(_ context.Context, _ Inbound, envelope string) (api.Receipt, error) {
		modelInput = envelope
		return api.Receipt{Outcome: "accepted"}, nil
	})
	feedback := s.Handle(t.Context(), Inbound{Channel: "weixin", Conversation: "paired", ID: "reply", Text: "/answer Q2 自己写的答案"}, api.Snapshot{RuntimeOwner: "owner", Connection: "ready"})
	if !strings.Contains(feedback, "已提交") || !strings.Contains(modelInput, `"answer":"自己写的答案"`) || !strings.Contains(modelInput, `"questionItemId":"[\"request_user_input_async\",\"call\",1]"`) {
		t.Fatalf("wrong field reply: %q %q", feedback, modelInput)
	}
	card, err = s.AsyncCard(item, "owner")
	if err != nil || !strings.Contains(card.Text, "[Q1] 第一题\n[1] 甲") || !strings.Contains(card.Text, "[Q2] 第二题\n回答已提交") {
		t.Fatalf("sibling status changed: %q %v", card.Text, err)
	}
}
