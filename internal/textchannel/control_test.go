package textchannel

import (
	"context"
	"fmt"
	"strings"
	"testing"

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
	commands := cardCommands(card, "/approve ")
	if len(commands) != 1 || commands[0] != "/approve "+id+" 1" || !strings.Contains(card, "[2] 允许（范围：仅本次）") || !strings.Contains(card, "[3] 允许（范围：始终）") {
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
			commands := cardCommands(card, "/approve ")
			if len(commands) != 1 || commands[0] != fmt.Sprintf("/approve %s 1", id) || !strings.Contains(card, fmt.Sprintf("[%d] %s", index+1, meaning(option{ID: choice.ID, Label: choice.Label, Scope: choice.Scope}))) {
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
	if err != nil || !strings.Contains(card, "回答请输入 /answer "+id+" 后接完整回答。") {
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
			commands := cardCommands(card, "/answer ")
			if len(commands) != 1 || commands[0] != fmt.Sprintf("/answer %s 1", id) || !strings.Contains(card, fmt.Sprintf("[%d] %s", index+1, choice.Label)) {
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
