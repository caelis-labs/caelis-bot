package textchannel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestTextApprovalCatalogClaimAndRestart(t *testing.T) {
	root := t.TempDir()
	var sent []api.Decision
	decide := func(_ context.Context, d api.Decision) error { sent = append(sent, d); return nil }
	s, err := Open(root, decide)
	if err != nil {
		t.Fatal(err)
	}
	a := api.Approval{ID: "native", TurnKey: "turn", Owner: "worker", Status: "pending", Choices: []api.Choice{{ID: "deny", Label: "拒绝"}, {ID: "allow-once", Label: "允许", Scope: "once"}, {ID: "allow-all", Label: "允许", Scope: "always"}}}
	card, id, err := s.Card(a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(card, "提示：发送 /approve "+id+" + 选项标号") || !strings.Contains(card, "[2] 允许\n") || !strings.Contains(card, "[3] 允许\n") || strings.Contains(card, "（范围：") {
		t.Fatal(card)
	}
	snap := api.Snapshot{Approvals: []api.Approval{a}}
	in := Inbound{Channel: "weixin", Conversation: "owner\x00ctx", ID: "msg-1", Text: "/approve " + id + " 2"}
	if got := s.Handle(t.Context(), in, snap); got != "决定已提交，等待 Runtime 确认。" {
		t.Fatal(got)
	}
	reopened, err := Open(root, decide)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Handle(t.Context(), in, snap); got != "决定已提交，等待 Runtime 确认。" {
		t.Fatal(got)
	}
	in.ID = "msg-2"
	in.Text = "/approve " + id + " 3"
	if got := reopened.Handle(t.Context(), in, snap); !strings.Contains(got, "已提交") {
		t.Fatal(got)
	}
	if len(sent) != 1 || sent[0].Choice != "allow-once" || sent[0].ID != "native" {
		t.Fatalf("decisions: %#v", sent)
	}
	a.Choices[1], a.Choices[2] = a.Choices[2], a.Choices[1]
	snap.Approvals[0] = a
	if _, next, err := reopened.Card(a); err != nil || next == id {
		t.Fatalf("reused changed index: %s %v", next, err)
	}
}

func TestShortApprovalNumbersRecycleOnlyAfterOriginalSettles(t *testing.T) {
	root := t.TempDir()
	var sent []api.Decision
	s, err := Open(root, func(_ context.Context, d api.Decision) error { sent = append(sent, d); return nil })
	if err != nil {
		t.Fatal(err)
	}
	old := api.Approval{ID: "old-native", TurnKey: "old-turn", Owner: "conversation", Status: "pending", Choices: []api.Choice{{ID: "deny", Label: "拒绝"}, {ID: "allow", Label: "允许"}}}
	_, oldID, err := s.Card(old)
	if err != nil || oldID != "A1" {
		t.Fatal(oldID, err)
	}
	s.state.NextApproval = shortIDPoolSize
	active := old
	active.ID = "active-native"
	_, activeID, err := s.Card(active)
	if err != nil || activeID != "A2" {
		t.Fatal("pending slot reused", activeID, err)
	}
	if got := s.Handle(t.Context(), Inbound{Channel: "weixin", Conversation: "owner", ID: "old-answer", Text: "/approve A1 1"}, api.Snapshot{Approvals: []api.Approval{old}}); !strings.Contains(got, "已提交") {
		t.Fatal(got)
	}
	reopened, err := Open(root, func(_ context.Context, d api.Decision) error { sent = append(sent, d); return nil })
	if err != nil {
		t.Fatal(err)
	}
	reopened.state.NextApproval = shortIDPoolSize
	fresh := old
	fresh.ID, fresh.TurnKey = "fresh-native", "fresh-turn"
	_, freshID, err := reopened.Card(fresh)
	if err != nil || freshID != "A1" {
		t.Fatal("settled slot not returned to tail", freshID, err)
	}
	// A bare stale command is intentionally interpreted against the newer card.
	if got := reopened.Handle(t.Context(), Inbound{Channel: "telegram", Conversation: "owner", ID: "new-answer", Text: "/approve A1 2"}, api.Snapshot{Approvals: []api.Approval{fresh}}); !strings.Contains(got, "已提交") {
		t.Fatal(got)
	}
	if len(sent) != 2 || sent[0].ID != old.ID || sent[0].Choice != "deny" || sent[1].ID != fresh.ID || sent[1].Choice != "allow" {
		t.Fatal(sent)
	}
}

func TestExpiredApprovalNumberReturnsToQueue(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	old := api.Approval{ID: "expired", TurnKey: "old", Owner: "conversation", Status: "pending", Choices: []api.Choice{{ID: "deny", Label: "拒绝"}}}
	_, id, _ := s.Card(old)
	if err := s.Reconcile(api.Snapshot{Connection: "ready"}); err != nil {
		t.Fatal(err)
	}
	s.state.NextApproval = shortIDPoolSize
	fresh := old
	fresh.ID, fresh.TurnKey = "fresh", "new"
	_, next, err := s.Card(fresh)
	if err != nil || next != id {
		t.Fatal(next, err)
	}
}

func TestUnansweredApprovalShortIDExpiresWithoutDecidingNativeRequest(t *testing.T) {
	s, _ := Open(t.TempDir(), func(context.Context, api.Decision) error {
		t.Fatal("expiry must not decide")
		return nil
	})
	old := api.Approval{ID: "still-pending", TurnKey: "turn", Owner: "conversation", Status: "pending", Choices: []api.Choice{{ID: "allow", Label: "允许"}}}
	_, id, _ := s.Card(old)
	p := s.state.Prompts[id]
	p.IssuedAt = time.Now().Add(-shortIDMaxAge - time.Hour).Unix()
	s.state.Prompts[id] = p
	if got := s.Handle(t.Context(), Inbound{Channel: "weixin", Conversation: "owner", ID: "late", Text: "/approve A1 1"}, api.Snapshot{Approvals: []api.Approval{old}}); !strings.Contains(got, "已过期") {
		t.Fatal(got)
	}
	if err := s.Reconcile(api.Snapshot{Connection: "ready", Approvals: []api.Approval{old}}); err != nil {
		t.Fatal(err)
	}
	s.state.NextApproval = shortIDPoolSize
	fresh := old
	fresh.ID, fresh.TurnKey = "next-native", "next-turn"
	_, next, err := s.Card(fresh)
	if err != nil || next != id {
		t.Fatal(next, err)
	}
}

func TestRoutineSuccessReceiptIsDurableWithoutExtraChatNotice(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var observed []Notice
	s.SetNoticeObserver(func(n Notice) { observed = append(observed, n) })
	in := Inbound{Channel: "weixin", Conversation: "owner", ID: "reply-1"}
	const ack = "回答已提交，等待后续回复。"
	if got := s.Publish(in, ack); got != ack {
		t.Fatal(got)
	}
	if got := s.Publish(in, ack); got != ack {
		t.Fatal(got)
	}
	if len(observed) != 0 || len(s.Notices()) != 0 {
		t.Fatalf("routine receipt consumed a message: %+v %+v", observed, s.Notices())
	}
	in.ID = "stale-command"
	if got := s.Publish(in, "原请求已处理。"); got != "原请求已处理。" || len(observed) != 1 || len(s.Notices()) != 1 {
		t.Fatalf("stale command lost feedback: %q %+v", got, observed)
	}
}

func TestEveryGeneratedApprovalOptionLineSubmitsItsNativeID(t *testing.T) {
	choices := []api.Choice{{ID: "deny", Label: "拒绝"}, {ID: "allow-once", Label: "允许", Scope: "once"}, {ID: "allow-all", Label: "允许", Scope: "always"}}
	for index, choice := range choices {
		t.Run(choice.ID, func(t *testing.T) {
			var sent []api.Decision
			s, err := Open(t.TempDir(), func(_ context.Context, d api.Decision) error { sent = append(sent, d); return nil })
			if err != nil {
				t.Fatal(err)
			}
			a := api.Approval{ID: "native", TurnKey: "turn", Owner: "worker", Status: "pending", Choices: choices}
			card, id, err := s.Card(a)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(card, "提示：发送 /approve "+id+" + 选项标号") || !strings.Contains(card, fmt.Sprintf("[%d] %s", index+1, choice.Label)) {
				t.Fatal(card)
			}
			command := fmt.Sprintf("/approve %s %d", id, index+1)
			got := s.Handle(t.Context(), Inbound{Channel: "weixin", Conversation: "ctx", ID: "copy", Text: command}, api.Snapshot{Approvals: []api.Approval{a}})
			if !strings.Contains(got, "已提交") || len(sent) != 1 || sent[0].Choice != choice.ID {
				t.Fatalf("copy %q: %q, decisions %#v", command, got, sent)
			}
		})
	}
}

func TestQuestionFreeTextAndExpiredRequest(t *testing.T) {
	var sent []api.Decision
	s, _ := Open(t.TempDir(), func(_ context.Context, d api.Decision) error { sent = append(sent, d); return nil })
	a := api.Approval{ID: "worker-question", TurnKey: "child", Owner: "task-1", Status: "pending", Questions: []api.Question{{ID: "q", Title: "选择或说明", Type: "text", Required: true, Options: []api.Choice{{ID: "one", Label: "第一项"}, {ID: "two", Label: "第二项"}}}}}
	card, id, err := s.Card(a)
	if err != nil || !strings.Contains(card, "提示：发送 /answer "+id+" + 选项标号或完整回答") {
		t.Fatalf("%q %v", card, err)
	}
	snap := api.Snapshot{Approvals: []api.Approval{a}}
	in := Inbound{Channel: "weixin", Conversation: "ctx", ID: "1", Text: "/answer " + id + " 2 详细说明"}
	if got := s.Handle(t.Context(), in, snap); !strings.Contains(got, "已提交") {
		t.Fatal(got)
	}
	if len(sent) != 1 || sent[0].Choice != "answer" || sent[0].Answers["q"][0] != "2 详细说明" {
		t.Fatalf("custom answer: %#v", sent)
	}
	in.ID = "2"
	in.Text = "/answer " + id + " 1"
	snap.Approvals[0].Status = "resolved"
	if got := s.Handle(t.Context(), in, snap); !strings.Contains(got, "已处理") {
		t.Fatal(got)
	} // original claim, never replay
	if len(sent) != 1 {
		t.Fatal(sent)
	}
}

func TestGeneratedQuestionOptionLineSubmitsExactNativeOption(t *testing.T) {
	choices := []api.Choice{{ID: "inspect", Label: "先检查"}, {ID: "continue", Label: "继续"}}
	for index, choice := range choices {
		t.Run(choice.ID, func(t *testing.T) {
			var sent []api.Decision
			s, err := Open(t.TempDir(), func(_ context.Context, d api.Decision) error { sent = append(sent, d); return nil })
			if err != nil {
				t.Fatal(err)
			}
			a := api.Approval{ID: "worker-question", TurnKey: "child", Owner: "task", Status: "pending", Questions: []api.Question{{ID: "question", Title: "选择下一步", Type: "text", Required: true, Options: choices}}}
			card, id, err := s.Card(a)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(card, "提示：发送 /answer "+id+" + 选项标号或完整回答") || !strings.Contains(card, fmt.Sprintf("[%d] %s", index+1, choice.Label)) {
				t.Fatal(card)
			}
			command := fmt.Sprintf("/answer %s %d", id, index+1)
			got := s.Handle(t.Context(), Inbound{Channel: "weixin", Conversation: "ctx", ID: "copy", Text: command}, api.Snapshot{Approvals: []api.Approval{a}})
			if !strings.Contains(got, "已提交") || len(sent) != 1 || sent[0].ID != "worker-question" || sent[0].Choice != "answer" || len(sent[0].Answers["question"]) != 1 || sent[0].Answers["question"][0] != choice.ID {
				t.Fatalf("copy %q: %q, decisions %#v", command, got, sent)
			}
		})
	}
}

func TestCustomApprovalOpinionDoesNotClaimOrPretendToForward(t *testing.T) {
	var sent []api.Decision
	s, _ := Open(t.TempDir(), func(_ context.Context, d api.Decision) error { sent = append(sent, d); return nil })
	a := api.Approval{ID: "native", TurnKey: "turn", Owner: "conversation", Status: "pending", Choices: []api.Choice{{ID: "deny", Label: "拒绝"}, {ID: "allow", Label: "允许"}}}
	card, id, err := s.Card(a)
	if err != nil || strings.Contains(card, "修改计划") {
		t.Fatalf("card %q: %v", card, err)
	}
	got := s.Handle(t.Context(), Inbound{Channel: "telegram", Conversation: "chat", ID: "opinion", Text: "/approve " + id + " 请缩小范围"}, api.Snapshot{Approvals: []api.Approval{a}})
	if !strings.Contains(got, "不会转交 Bot") || len(sent) != 0 {
		t.Fatalf("opinion %q dispatched %#v", got, sent)
	}
}

func cardCommands(card, prefix string) []string {
	var commands []string
	for _, line := range strings.Split(card, "\n") {
		if strings.HasPrefix(line, prefix) {
			commands = append(commands, line)
		}
	}
	return commands
}

func TestRouteUsesSavedOriginAndFailsClosedOnMixedTurn(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	_ = s.RecordOrigin("tg-1", Origin{"telegram", "7"})
	_ = s.RecordOrigin("wx-1", Origin{"weixin", "owner\x00ctx"})
	snap := api.Snapshot{Items: []api.Item{{Kind: "user", RequestID: "tg-1", TurnKey: "turn"}, {Kind: "user", RequestID: "wx-1", TurnKey: "turn"}}}
	if _, ok := s.Route(snap, "", "turn"); ok {
		t.Fatal("ambiguous steering was routed")
	}
	if got, ok := s.Route(snap, "wx-1", "turn"); !ok || got.Channel != "weixin" {
		t.Fatalf("direct request: %#v %v", got, ok)
	}
	if _, ok := s.Route(snap, "", "late-child"); ok {
		t.Fatal("guessed late worker result")
	}
}
